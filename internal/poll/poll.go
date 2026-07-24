package poll

import (
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


	rows, err := storage.DB.Query(
		`
		SELECT id, text
		FROM answers
		WHERE poll_id = ?
		ORDER BY display_order ASC
		`,
		pollID,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()


	type answerCount struct {
		text  string
		votes int64
	}


	counts := make(map[int64]*answerCount)
	order := make([]int64, 0)


	for rows.Next() {

		var (
			id   int64
			text string
		)

		if err := rows.Scan(&id, &text); err != nil {
			return nil, err
		}

		order = append(order, id)

		counts[id] = &answerCount{
			text: text,
		}
	}


	voteRows, err := storage.DB.Query(
		`
		SELECT answer_ids
		FROM votes
		WHERE poll_id = ?
		`,
		pollID,
	)

	if err != nil {
		return nil, err
	}

	defer voteRows.Close()


	for voteRows.Next() {

		var answerJSON string

		if err := voteRows.Scan(&answerJSON); err != nil {
			continue
		}


		var ids []int64

		if err := json.Unmarshal(
			[]byte(answerJSON),
			&ids,
		); err != nil {
			continue
		}


		for _, id := range ids {

			if answer, ok := counts[id]; ok {
				answer.votes++
			}
		}
	}


	for _, id := range order {

		answer := counts[id]

		stats.Labels = append(
			stats.Labels,
			answer.text,
		)

		stats.Values = append(
			stats.Values,
			answer.votes,
		)
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
func CastVote(
	pollID int64,
	cpf string,
	answerIDs []int64,
) *VoteError {


	cpf = strings.TrimSpace(cpf)


	if cpf == "" || len(answerIDs) == 0 {

		return &VoteError{
			Status: http.StatusBadRequest,
			Message: "cpf and answer_ids required",
		}
	}



	var (
		pollType string
		startDate string
		endDate string
	)


	err := storage.DB.QueryRow(
		`
		SELECT type,start_date,end_date
		FROM polls
		WHERE id = ?
		`,
		pollID,
	).Scan(
		&pollType,
		&startDate,
		&endDate,
	)


	if err != nil {

		if err == sql.ErrNoRows {
			return &VoteError{
				Status:http.StatusNotFound,
				Message:"poll not found",
			}
		}


		return &VoteError{
			Status:http.StatusInternalServerError,
			Message:"database error",
		}
	}



	if !IsActive(startDate,endDate) {

		return &VoteError{
			Status:http.StatusGone,
			Message:"poll is no longer active",
		}
	}



	if pollType == "radio" && len(answerIDs) > 1 {

		return &VoteError{
			Status:http.StatusBadRequest,
			Message:"radio poll accepts only one answer",
		}
	}



	for _, answerID := range answerIDs {

		var id int64

		err := storage.DB.QueryRow(
			`
			SELECT id
			FROM answers
			WHERE id = ?
			AND poll_id = ?
			`,
			answerID,
			pollID,
		).Scan(&id)


		if err != nil {

			return &VoteError{
				Status:http.StatusBadRequest,
				Message:fmt.Sprintf(
					"answer %d not found",
					answerID,
				),
			}
		}
	}



	voterHash := security.HashCPF(cpf)


	var existing int64

	err = storage.DB.QueryRow(
		`
		SELECT id
		FROM votes
		WHERE poll_id = ?
		AND voter_hash = ?
		`,
		pollID,
		voterHash,
	).Scan(&existing)



	if err == nil {

		return &VoteError{
			Status:http.StatusConflict,
			Message:"cpf already voted",
		}
	}


	if err != sql.ErrNoRows {

		return &VoteError{
			Status:http.StatusInternalServerError,
			Message:"db error",
		}
	}



	answerJSON, err := json.Marshal(answerIDs)

	if err != nil {

		return &VoteError{
			Status:http.StatusInternalServerError,
			Message:"failed encoding vote",
		}
	}



	now := time.Now().
		UTC().
		Format(time.RFC3339)



	_, err = storage.DB.Exec(
		`
		INSERT INTO votes
		(
		 poll_id,
		 voter_hash,
		 answer_ids,
		 voted_at
		)
		VALUES(?,?,?,?)
		`,
		pollID,
		voterHash,
		string(answerJSON),
		now,
	)


	if err != nil {

		return &VoteError{
			Status:http.StatusInternalServerError,
			Message:"failed saving vote",
		}
	}



	_, err = storage.DB.Exec(
		`
		UPDATE voters
		SET passcode = NULL,
		    used_at = ?
		WHERE cpf = ?
		`,
		now,
		cpf,
	)


	if err != nil {

		return &VoteError{
			Status:http.StatusInternalServerError,
			Message:"failed updating voter",
		}
	}



	storage.LogAction(
		"VOTE_SUBMITTED",
		fmt.Sprintf("PollID: %d", pollID),
	)


	return nil
}
