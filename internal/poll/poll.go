// Package poll holds the core voting business logic: poll activity checks,
// permission checks, vote casting and result statistics aggregation.
package poll

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	sqinn "github.com/cvilsmeier/sqinn-go/v2"

	"github.com/waldirborbajr/govote/internal/models"
	"github.com/waldirborbajr/govote/internal/security"
	"github.com/waldirborbajr/govote/internal/storage"
)

// IsActive reports whether now (UTC) is within the poll's start/end window.
func IsActive(startDate, endDate string) bool {
	now := time.Now().UTC()
	start, err1 := time.Parse(time.RFC3339, startDate)
	end, err2 := time.Parse(time.RFC3339, endDate)
	if err1 != nil || err2 != nil {
		return false
	}
	return now.After(start) && now.Before(end)
}

// CanAccessPoll reports whether the admin may access the given poll (super
// admins can access any poll, others only their own).
func CanAccessPoll(adminID int64, isSuper bool, pollID int64) bool {
	if isSuper {
		return true
	}
	// Verifica se a enquete pertence ao admin
	rows, err := storage.DB.QueryRows("SELECT id FROM polls WHERE id = ? AND created_by = ?",
		[]sqinn.Value{sqinn.Int64Value(pollID), sqinn.Int64Value(adminID)},
		[]byte{sqinn.ValInt64})

	return err == nil && len(rows) > 0
}

// GetPollStats returns aggregate statistics for a poll, enforcing per-admin
// access control unless the caller is a super admin.
func GetPollStats(pollID int64, adminID int64, isSuper bool) (*models.PollStats, error) {
	stats := &models.PollStats{}

	// Verifica permissão
	if !isSuper {
		rows, err := storage.DB.QueryRows("SELECT id FROM polls WHERE id = ? AND created_by = ?",
			[]sqinn.Value{sqinn.Int64Value(pollID), sqinn.Int64Value(adminID)},
			[]byte{sqinn.ValInt64})
		if err != nil || len(rows) == 0 {
			return nil, fmt.Errorf("acesso negado ou enquete não encontrada")
		}
	}

	// Título do Poll
	rows, _ := storage.DB.QueryRows("SELECT title FROM polls WHERE id = ?",
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValString})
	if len(rows) == 0 {
		return nil, fmt.Errorf("enquete não encontrada")
	}
	stats.PollTitle = rows[0][0].String

	// Total de votos
	rows, _ = storage.DB.QueryRows("SELECT count(*) FROM votes WHERE poll_id = ?",
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValInt64})
	if len(rows) == 0 {
		return nil, fmt.Errorf("erro ao contar votos")
	}
	stats.TotalVotes = rows[0][0].Int64

	// Votos por opção. NOTA: isso antes usava
	// `v.answer_ids LIKE '%' || a.id || '%'`, que é uma comparação de
	// substring sobre um ID numérico — a resposta de id=1 também "casava"
	// com votos em [10], [21], [1,10] etc, inflando a contagem. Em vez
	// disso, buscamos as respostas do poll e os answer_ids (JSON) de cada
	// voto e contamos em Go, com igualdade exata de ID (mesma lógica já
	// usada em api.HandleResults).
	arows, err := storage.DB.QueryRows(
		"SELECT id, text FROM answers WHERE poll_id = ? ORDER BY display_order ASC",
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValInt64, sqinn.ValString},
	)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar respostas: %w", err)
	}

	type answerCount struct {
		text  string
		votes int64
	}
	order := make([]int64, 0, len(arows))
	counts := make(map[int64]*answerCount, len(arows))
	for _, arow := range arows {
		id := arow[0].Int64
		order = append(order, id)
		counts[id] = &answerCount{text: arow[1].String}
	}

	vrows, err := storage.DB.QueryRows(
		"SELECT answer_ids FROM votes WHERE poll_id = ?",
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValString},
	)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar votos: %w", err)
	}

	for _, vrow := range vrows {
		var ids []int64
		if err := json.Unmarshal([]byte(vrow[0].String), &ids); err != nil {
			continue
		}
		for _, id := range ids {
			if ac, ok := counts[id]; ok {
				ac.votes++
			}
		}
	}

	for _, id := range order {
		ac := counts[id]
		stats.Labels = append(stats.Labels, ac.text)
		stats.Values = append(stats.Values, ac.votes)
	}

	return stats, nil
}

// VoteError carries an HTTP status and message describing a failed vote.
type VoteError struct {
	Status  int
	Message string
}

func (e *VoteError) Error() string { return e.Message }

// CastVote validates and records a vote for pollID by the voter identified by
// cpf. It returns a *VoteError on any validation/persistence failure.
func CastVote(pollID int64, cpf string, answerIDs []int64) *VoteError {
	if strings.TrimSpace(cpf) == "" || len(answerIDs) == 0 {
		return &VoteError{http.StatusBadRequest, "cpf and answer_ids required"}
	}

	prows, err := storage.DB.QueryRows(
		`SELECT type, start_date, end_date FROM polls WHERE id = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValString, sqinn.ValString, sqinn.ValString},
	)
	if err != nil || len(prows) == 0 {
		return &VoteError{http.StatusNotFound, "poll not found"}
	}

	voterHash := security.HashCPF(cpf)

	row := prows[0]
	pollType := row[0].String
	startDate := row[1].String
	endDate := row[2].String

	if !IsActive(startDate, endDate) {
		return &VoteError{http.StatusGone, "poll is no longer active"}
	}

	if pollType == "radio" && len(answerIDs) > 1 {
		return &VoteError{http.StatusBadRequest, "radio poll accepts only one answer"}
	}

	for _, ansID := range answerIDs {
		arows, err := storage.DB.QueryRows(
			`SELECT id FROM answers WHERE id = ? AND poll_id = ?`,
			[]sqinn.Value{sqinn.Int64Value(ansID), sqinn.Int64Value(pollID)},
			[]byte{sqinn.ValInt64},
		)
		if err != nil || len(arows) == 0 {
			return &VoteError{http.StatusBadRequest, fmt.Sprintf("answer %d not found", ansID)}
		}
	}

	vrows, err := storage.DB.QueryRows(
		`SELECT id FROM votes WHERE poll_id = ? AND voter_hash = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID), sqinn.StringValue(voterHash)},
		[]byte{sqinn.ValInt64},
	)
	if err != nil {
		return &VoteError{http.StatusInternalServerError, "db error"}
	}
	if len(vrows) > 0 {
		return &VoteError{http.StatusConflict, "cpf already voted"}
	}

	answerIDsJSON, _ := json.Marshal(answerIDs)
	now := time.Now().UTC().Format(time.RFC3339)

	storage.DB.MustExecParams(
		`INSERT INTO votes (poll_id, voter_hash, answer_ids, voted_at) VALUES (?, ?, ?, ?)`,
		1, 4,
		[]sqinn.Value{
			sqinn.Int64Value(pollID),
			sqinn.StringValue(voterHash),
			sqinn.StringValue(string(answerIDsJSON)),
			sqinn.StringValue(now),
		},
	)

	storage.DB.MustExecParams(
		`UPDATE voters SET passcode = NULL, used_at = ? WHERE cpf = ?`,
		1, 2,
		[]sqinn.Value{
			sqinn.StringValue(now),
			sqinn.StringValue(cpf),
		},
	)

	storage.LogAction("VOTE_SUBMITTED", fmt.Sprintf("PollID: %d", pollID))
	return nil
}
