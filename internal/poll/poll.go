package poll

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/waldirborbajr/govote/internal/models"
	"github.com/waldirborbajr/govote/internal/security"
	"github.com/waldirborbajr/govote/internal/storage"
)

// IsActive reports whether now (UTC) is within the poll's start/end window.
func IsActive(startDate, endDate string) bool {
	now := time.Now().UTC()

	start, err := time.Parse(time.RFC3339, startDate)
	if err != nil {
		return false
	}

	end, err := time.Parse(time.RFC3339, endDate)
	if err != nil {
		return false
	}

	return !now.Before(start) && !now.After(end)
}

// CanAccessPoll reports whether an admin can access a poll.
func CanAccessPoll(adminID int64, isSuper bool, pollID int64) bool {

	if isSuper {
		return true
	}

	var id int64

	err := storage.DB.QueryRow(
		`
		SELECT id
		FROM polls
		WHERE id = ?
		AND created_by = ?
		`,
		pollID,
		adminID,
	).Scan(&id)

	return err == nil
}

// GetResults returns per-answer vote counts for a poll, aggregated entirely
// in SQL via json_each() over votes.answer_ids (a JSON array column, needed
// because polls can be multi-select). This replaces the old pattern — used
// to be duplicated in three call sites — of fetching every vote row for the
// poll and tallying JSON arrays in Go, which meant transferring and
// unmarshaling O(total votes) rows on every single request (cache misses
// included). This query transfers and scans O(answers) rows instead,
// regardless of how many votes the poll has.
func GetResults(pollID int64) ([]models.ResultAnswer, error) {
	rows, err := storage.DB.Query(
		`
		SELECT a.id, a.text, COALESCE(v.cnt, 0) AS votes
		FROM answers a
		LEFT JOIN (
			SELECT je.value AS answer_id, COUNT(*) AS cnt
			FROM votes, json_each(votes.answer_ids) AS je
			WHERE votes.poll_id = ?
			GROUP BY je.value
		) v ON v.answer_id = a.id
		WHERE a.poll_id = ?
		ORDER BY a.display_order ASC
		`,
		pollID, pollID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.ResultAnswer
	for rows.Next() {
		var r models.ResultAnswer
		if err := rows.Scan(&r.ID, &r.Text, &r.Votes); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// GetPollStats returns aggregate statistics for a poll.
func GetPollStats(
	pollID int64,
	adminID int64,
	isSuper bool,
) (*models.PollStats, error) {

	stats := &models.PollStats{}

	if !isSuper {
		if !CanAccessPoll(adminID, false, pollID) {
			return nil, fmt.Errorf("acesso negado ou enquete não encontrada")
		}
	}

	err := storage.DB.QueryRow(
		`
		SELECT title
		FROM polls
		WHERE id = ?
		`,
		pollID,
	).Scan(&stats.PollTitle)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("enquete não encontrada")
		}

		return nil, err
	}

	err = storage.DB.QueryRow(
		`
		SELECT COUNT(*)
		FROM votes
		WHERE poll_id = ?
		`,
		pollID,
	).Scan(&stats.TotalVotes)

	if err != nil {
		return nil, fmt.Errorf("erro ao contar votos: %w", err)
	}

	results, err := GetResults(pollID)
	if err != nil {
		return nil, fmt.Errorf("erro ao agregar resultados: %w", err)
	}

	for _, r := range results {
		stats.Labels = append(stats.Labels, r.Text)
		stats.Values = append(stats.Values, int64(r.Votes))
	}

	return stats, nil
}

// VoteError carries HTTP status and message.
type VoteError struct {
	Status  int
	Message string
}

func (e *VoteError) Error() string {
	return e.Message
}

// CastVote validates and stores a vote.
// Validation reads run outside a transaction; the critical section
// (duplicate check + INSERT vote + UPDATE voter) runs in a single
// transaction to shorten lock time and close the race window under load.
func CastVote(
	pollID int64,
	cpf string,
	answerIDs []int64,
) *VoteError {

	cpf = strings.TrimSpace(cpf)

	if cpf == "" || len(answerIDs) == 0 {
		return &VoteError{
			Status:  http.StatusBadRequest,
			Message: "cpf and answer_ids required",
		}
	}

	var verifiedAt sql.NullString
	if err := storage.DB.QueryRow(
		`SELECT verified_at FROM voters WHERE cpf = ?`,
		cpf,
	).Scan(&verifiedAt); err != nil || !verifiedAt.Valid || verifiedAt.String == "" {
		return &VoteError{
			Status:  http.StatusUnauthorized,
			Message: "eleitor não verificado",
		}
	}

	var (
		pollType  string
		startDate string
		endDate   string
	)

	err := storage.DB.QueryRow(
		`SELECT type, start_date, end_date FROM polls WHERE id = ?`,
		pollID,
	).Scan(&pollType, &startDate, &endDate)

	if err != nil {
		if err == sql.ErrNoRows {
			return &VoteError{
				Status:  http.StatusNotFound,
				Message: "poll not found",
			}
		}
		return &VoteError{
			Status:  http.StatusInternalServerError,
			Message: "database error",
		}
	}

	if !IsActive(startDate, endDate) {
		return &VoteError{
			Status:  http.StatusGone,
			Message: "poll is no longer active",
		}
	}

	if pollType == "radio" && len(answerIDs) > 1 {
		return &VoteError{
			Status:  http.StatusBadRequest,
			Message: "radio poll accepts only one answer",
		}
	}

	if voteErr := validateAnswerIDs(pollID, answerIDs); voteErr != nil {
		return voteErr
	}

	voterHash := security.HashCPF(cpf)

	answerJSON, err := json.Marshal(answerIDs)
	if err != nil {
		return &VoteError{
			Status:  http.StatusInternalServerError,
			Message: "failed encoding vote",
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)

	ctx := context.Background()

	// A dedicated *sql.Conn pins the whole transaction to a single physical
	// SQLite connection, which lets us issue a raw BEGIN IMMEDIATE below
	// instead of relying on driver-specific DSN flags for it.
	conn, err := storage.DB.Conn(ctx)
	if err != nil {
		return &VoteError{
			Status:  http.StatusInternalServerError,
			Message: "database error",
		}
	}
	defer conn.Close()

	// BEGIN IMMEDIATE acquires SQLite's write lock up front. The default
	// (BEGIN DEFERRED, what tx.Begin() issues) starts as a read and only
	// tries to upgrade to a write lock on the first write statement — if
	// that upgrade loses the race to another writer, everything the
	// transaction already read (the duplicate-vote check, below) is thrown
	// away and retried inside busy_timeout. Under Black Friday load several
	// OS processes (API replicas doing auth writes + the vote-worker) share
	// this same SQLite file, so that race is routine, not an edge case.
	// IMMEDIATE fails fast/serializes cleanly against them instead.
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return &VoteError{
			Status:  http.StatusInternalServerError,
			Message: "database error",
		}
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	var existing int64
	err = conn.QueryRowContext(ctx,
		`SELECT id FROM votes WHERE poll_id = ? AND voter_hash = ?`,
		pollID, voterHash,
	).Scan(&existing)
	if err == nil {
		return &VoteError{
			Status:  http.StatusConflict,
			Message: "cpf already voted",
		}
	}
	if err != sql.ErrNoRows {
		return &VoteError{
			Status:  http.StatusInternalServerError,
			Message: "db error",
		}
	}

	if _, err = conn.ExecContext(ctx,
		`INSERT INTO votes (poll_id, voter_hash, answer_ids, voted_at) VALUES (?,?,?,?)`,
		pollID, voterHash, string(answerJSON), now,
	); err != nil {
		return &VoteError{
			Status:  http.StatusInternalServerError,
			Message: "failed saving vote",
		}
	}

	if _, err = conn.ExecContext(ctx,
		`UPDATE voters SET passcode = NULL, used_at = ? WHERE cpf = ?`,
		now, cpf,
	); err != nil {
		return &VoteError{
			Status:  http.StatusInternalServerError,
			Message: "failed updating voter",
		}
	}

	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return &VoteError{
			Status:  http.StatusInternalServerError,
			Message: "failed committing vote",
		}
	}
	committed = true

	storage.LogAction("VOTE_SUBMITTED", fmt.Sprintf("PollID: %d", pollID))
	return nil
}

// validateAnswerIDs confirms every answerID belongs to pollID using a single
// batched query instead of one round trip per answer.
func validateAnswerIDs(pollID int64, answerIDs []int64) *VoteError {
	placeholders := make([]string, len(answerIDs))
	args := make([]any, 0, len(answerIDs)+1)
	args = append(args, pollID)
	for i, id := range answerIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	rows, err := storage.DB.Query(
		`SELECT id FROM answers WHERE poll_id = ? AND id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return &VoteError{Status: http.StatusInternalServerError, Message: "database error"}
	}
	defer rows.Close()

	found := make(map[int64]bool, len(answerIDs))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return &VoteError{Status: http.StatusInternalServerError, Message: "database error"}
		}
		found[id] = true
	}
	if err := rows.Err(); err != nil {
		return &VoteError{Status: http.StatusInternalServerError, Message: "database error"}
	}

	for _, id := range answerIDs {
		if !found[id] {
			return &VoteError{
				Status:  http.StatusBadRequest,
				Message: fmt.Sprintf("answer %d not found", id),
			}
		}
	}
	return nil
}
