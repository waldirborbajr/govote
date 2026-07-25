// api/handlers.go
// Package api implements the JSON HTTP handlers for the public voting API and
// the admin statistics endpoint.
package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/waldirborbajr/govote/internal/models"
	"github.com/waldirborbajr/govote/internal/notify"
	"github.com/waldirborbajr/govote/internal/poll"
	"github.com/waldirborbajr/govote/internal/security"
	"github.com/waldirborbajr/govote/internal/storage"
	"github.com/waldirborbajr/govote/internal/web"
)

// HandleRequestPasscode generates and stores a voter passcode and returns a
// WhatsApp deep link to deliver it.
func HandleRequestPasscode(w http.ResponseWriter, r *http.Request) {
	var req models.RequestPasscodeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.RespondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if strings.TrimSpace(req.CPF) == "" || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Phone) == "" {
		web.RespondError(w, http.StatusBadRequest, "cpf, name, phone required")
		return
	}

	passcode := security.GeneratePasscode()

	if _, err := storage.DB.Exec(
		`INSERT INTO voters (cpf, name, phone, passcode, verified_at)
		 VALUES (?, ?, ?, ?, NULL)
		 ON CONFLICT(cpf) DO UPDATE SET passcode=excluded.passcode`,
		req.CPF,
		req.Name,
		req.Phone,
		security.HashPasscode(passcode),
	); err != nil {
		web.RespondError(w, http.StatusInternalServerError, "db error")
		return
	}

	whatsappURL := notify.BuildWhatsAppURL(req.Phone, passcode)
	fmt.Printf("[PoC] CPF %s passcode: %s\n", req.CPF, passcode)

	web.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"whatsapp_url": whatsappURL,
		"message":      "Código gerado com sucesso!",
	})
}

// HandleVerify validates a voter's passcode and marks it as used.
func HandleVerify(w http.ResponseWriter, r *http.Request) {
	var req models.VerifyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.RespondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if strings.TrimSpace(req.CPF) == "" || strings.TrimSpace(req.Passcode) == "" {
		web.RespondError(w, http.StatusBadRequest, "cpf and passcode required")
		return
	}

	var storedHash, usedAt sql.NullString
	err := storage.DB.QueryRow(
		`SELECT passcode, used_at FROM voters WHERE cpf = ?`,
		req.CPF,
	).Scan(&storedHash, &usedAt)
	if err != nil {
		web.RespondError(w, http.StatusUnauthorized, "cpf not found")
		return
	}

	if !storedHash.Valid || storedHash.String == "" || !security.CheckPasscode(storedHash.String, req.Passcode) {
		web.RespondError(w, http.StatusUnauthorized, "wrong passcode")
		return
	}

	if usedAt.Valid && usedAt.String != "" {
		web.RespondError(w, http.StatusUnauthorized, "este código já foi utilizado. Solicite um novo.")
		return
	}

	storage.DB.Exec(
		`UPDATE voters SET passcode = NULL, used_at = ? WHERE cpf = ?`,
		time.Now().UTC().Format(time.RFC3339),
		req.CPF,
	)

	web.RespondJSON(w, http.StatusOK, map[string]interface{}{"verified": true, "cpf": req.CPF})
}

// HandleListPolls returns the currently active polls with their answers.
func HandleListPolls(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339)

	rows, err := storage.DB.Query(
		`SELECT id, title, type, start_date, end_date, created_by, created_at
		 FROM polls
		 WHERE start_date <= ? AND end_date >= ?
		 ORDER BY created_at DESC`,
		now, now,
	)
	if err != nil {
		web.RespondError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	var polls []models.Poll
	for rows.Next() {
		var p models.Poll
		if err := rows.Scan(&p.ID, &p.Title, &p.Type, &p.StartDate, &p.EndDate, &p.CreatedBy, &p.CreatedAt); err != nil {
			web.RespondError(w, http.StatusInternalServerError, "db error")
			return
		}

		answers, err := fetchAnswers(p.ID)
		if err != nil {
			web.RespondError(w, http.StatusInternalServerError, "db error")
			return
		}
		p.Answers = answers
		polls = append(polls, p)
	}

	if polls == nil {
		polls = []models.Poll{}
	}
	web.RespondJSON(w, http.StatusOK, polls)
}

// fetchAnswers returns the answers for a poll, ordered by display_order.
func fetchAnswers(pollID int64) ([]models.Answer, error) {
	arows, err := storage.DB.Query(
		`SELECT id, poll_id, text, display_order FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
		pollID,
	)
	if err != nil {
		return nil, err
	}
	defer arows.Close()

	var answers []models.Answer
	for arows.Next() {
		var a models.Answer
		if err := arows.Scan(&a.ID, &a.PollID, &a.Text, &a.DisplayOrder); err != nil {
			return nil, err
		}
		answers = append(answers, a)
	}
	return answers, nil
}

// HandleGetPoll returns a single active poll with its answers.
func HandleGetPoll(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/polls/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		web.RespondError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	var p models.Poll
	err = storage.DB.QueryRow(
		`SELECT id, title, type, start_date, end_date, created_by, created_at FROM polls WHERE id = ?`,
		id,
	).Scan(&p.ID, &p.Title, &p.Type, &p.StartDate, &p.EndDate, &p.CreatedBy, &p.CreatedAt)
	if err != nil {
		web.RespondError(w, http.StatusNotFound, "poll not found")
		return
	}

	if !poll.IsActive(p.StartDate, p.EndDate) {
		web.RespondError(w, http.StatusGone, "poll is no longer active")
		return
	}

	answers, err := fetchAnswers(p.ID)
	if err != nil {
		web.RespondError(w, http.StatusInternalServerError, "db error")
		return
	}
	p.Answers = answers
	web.RespondJSON(w, http.StatusOK, p)
}

// HandleCreatePoll creates a poll (and its answers) for the authenticated admin.
func HandleCreatePoll(w http.ResponseWriter, r *http.Request) {
	admin, err := web.GetAuthenticatedAdmin(r)
	if err != nil {
		web.RespondError(w, http.StatusUnauthorized, "Unauthorized admin connection")
		return
	}

	var req models.CreatePollReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.RespondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if strings.TrimSpace(req.Title) == "" || (req.Type != "radio" && req.Type != "checkbox") || len(req.Answers) == 0 {
		web.RespondError(w, http.StatusBadRequest, "title, type (radio/checkbox), and answers required")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	res, err := storage.DB.Exec(
		`INSERT INTO polls (title, type, start_date, end_date, allow_blank, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		req.Title,
		req.Type,
		req.StartDate,
		req.EndDate,
		storage.BoolToInt(req.AllowBlank),
		admin.ID,
		now,
	)
	if err != nil {
		web.RespondError(w, http.StatusInternalServerError, "error creating poll")
		return
	}

	lastInsertID, err := res.LastInsertId()
	if err != nil {
		web.RespondError(w, http.StatusInternalServerError, "error retrieving poll id")
		return
	}

	for i, answer := range req.Answers {
		text := strings.TrimSpace(answer.Text)
		if text == "" {
			continue
		}

		storage.DB.Exec(
			`INSERT INTO answers (poll_id, text, display_order) VALUES (?, ?, ?)`,
			lastInsertID,
			text,
			i,
		)
	}

	HandleListPolls(w, r)
}

// HandleVote records a vote submitted through the JSON API.
func HandleVote(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/polls/")
	idStr = strings.TrimSuffix(idStr, "/vote")
	pollID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		web.RespondError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	var req models.VoteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.RespondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if voteErr := poll.CastVote(pollID, req.CPF, req.AnswerIDs); voteErr != nil {
		web.RespondError(w, voteErr.Status, voteErr.Message)
		return
	}

	web.RespondJSON(w, http.StatusCreated, map[string]bool{"voted": true})
}

// HandleResults returns the tallied results for a poll.
func HandleResults(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/polls/")
	idStr = strings.TrimSuffix(idStr, "/results")
	pollID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		web.RespondError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	var endDateStr string
	if err := storage.DB.QueryRow(`SELECT end_date FROM polls WHERE id = ?`, pollID).Scan(&endDateStr); err != nil {
		web.RespondError(w, http.StatusNotFound, "poll not found")
		return
	}
	pollEndDate, _ := time.Parse(time.RFC3339, endDateStr)

	arows, err := storage.DB.Query(
		`SELECT id, text FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
		pollID,
	)
	if err != nil {
		web.RespondError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer arows.Close()

	answerMap := make(map[int64]models.ResultAnswer)
	for arows.Next() {
		var id int64
		var text string
		if err := arows.Scan(&id, &text); err != nil {
			web.RespondError(w, http.StatusInternalServerError, "db error")
			return
		}
		answerMap[id] = models.ResultAnswer{ID: id, Text: text, Votes: 0}
	}

	vrows, err := storage.DB.Query(`SELECT answer_ids FROM votes WHERE poll_id = ?`, pollID)
	if err != nil {
		web.RespondError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer vrows.Close()

	for vrows.Next() {
		var answerJSON string
		if err := vrows.Scan(&answerJSON); err != nil {
			continue
		}
		var ids []int64
		json.Unmarshal([]byte(answerJSON), &ids)
		for _, id := range ids {
			if ans, ok := answerMap[id]; ok {
				ans.Votes++
				answerMap[id] = ans
			}
		}
	}

	var results []models.ResultAnswer
	for _, ans := range answerMap {
		results = append(results, ans)
	}

	if time.Now().After(pollEndDate) {
		notify.SimulateNotification(pollID, results)
	}

	web.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"poll_id": pollID,
		"answers": results,
	})
}

// HandleAdminStats returns global voting statistics used by the dashboard.
func HandleAdminStats(w http.ResponseWriter, r *http.Request) {
	var totalVotes int64
	if err := storage.DB.QueryRow("SELECT count(DISTINCT voter_hash) FROM votes").Scan(&totalVotes); err != nil {
		web.RespondError(w, http.StatusInternalServerError, "db error")
		return
	}

	// TODO: totalEligible ainda vem de uma variável de ambiente com valor
	// padrão arbitrário. O ideal é vir de uma fonte real de eleitores
	// elegíveis (ex.: contagem da tabela voters, ou integração com um
	// cadastro eleitoral), não de um número fixo — ver TODO #14 do PR.
	totalEligible := 1000.0
	if v := os.Getenv("GOVOTE_TOTAL_ELIGIBLE_VOTERS"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed > 0 {
			totalEligible = parsed
		}
	}
	turnout := (float64(totalVotes) / totalEligible) * 100

	trows, err := storage.DB.Query(
		`SELECT strftime('%Y-%m-%dT%H:00:00', voted_at) as hour, count(*)
         FROM votes GROUP BY hour ORDER BY hour ASC`,
	)

	var timeline []map[string]interface{}
	if err == nil {
		defer trows.Close()
		for trows.Next() {
			var hour string
			var count int64
			if err := trows.Scan(&hour, &count); err != nil {
				continue
			}
			timeline = append(timeline, map[string]interface{}{
				"hour":  hour,
				"count": count,
			})
		}
	}
	if timeline == nil {
		timeline = []map[string]interface{}{}
	}

	web.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"total_votes": totalVotes,
		"turnout_pct": turnout,
		"timeline":    timeline,
	})
}
