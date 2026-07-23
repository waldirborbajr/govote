// Package api implements the JSON HTTP handlers for the public voting API and
// the admin statistics endpoint.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	sqinn "github.com/cvilsmeier/sqinn-go/v2"

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

	storage.DB.MustExecParams(
		`INSERT INTO voters (cpf, name, phone, passcode, verified_at)
		 VALUES (?, ?, ?, ?, NULL)
		 ON CONFLICT(cpf) DO UPDATE SET passcode=excluded.passcode`,
		1, 4,
		[]sqinn.Value{
			sqinn.StringValue(req.CPF),
			sqinn.StringValue(req.Name),
			sqinn.StringValue(req.Phone),
			sqinn.StringValue(security.HashPasscode(passcode)),
		},
	)

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

	rows, err := storage.DB.QueryRows(
		`SELECT passcode, used_at FROM voters WHERE cpf = ?`,
		[]sqinn.Value{sqinn.StringValue(req.CPF)},
		[]byte{sqinn.ValString, sqinn.ValString},
	)
	if err != nil || len(rows) == 0 {
		web.RespondError(w, http.StatusUnauthorized, "cpf not found")
		return
	}

	storedHash := rows[0][0].String
	usedAt := rows[0][1].String

	if storedHash == "" || !security.CheckPasscode(storedHash, req.Passcode) {
		web.RespondError(w, http.StatusUnauthorized, "wrong passcode")
		return
	}

	if usedAt != "" {
		web.RespondError(w, http.StatusUnauthorized, "este código já foi utilizado. Solicite um novo.")
		return
	}

	storage.DB.MustExecParams(
		`UPDATE voters SET passcode = NULL, used_at = ? WHERE cpf = ?`,
		1, 2,
		[]sqinn.Value{
			sqinn.StringValue(time.Now().UTC().Format(time.RFC3339)),
			sqinn.StringValue(req.CPF),
		},
	)

	web.RespondJSON(w, http.StatusOK, map[string]interface{}{"verified": true, "cpf": req.CPF})
}

// HandleListPolls returns the currently active polls with their answers.
func HandleListPolls(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339)

	rows, err := storage.DB.QueryRows(
		`SELECT id, title, type, start_date, end_date, created_by, created_at
		 FROM polls
		 WHERE start_date <= ? AND end_date >= ?
		 ORDER BY created_at DESC`,
		[]sqinn.Value{sqinn.StringValue(now), sqinn.StringValue(now)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValInt64, sqinn.ValString},
	)
	if err != nil {
		web.RespondError(w, http.StatusInternalServerError, "db error")
		return
	}

	var polls []models.Poll
	for _, row := range rows {
		var p models.Poll
		p.ID = row[0].Int64
		p.Title = row[1].String
		p.Type = row[2].String
		p.StartDate = row[3].String
		p.EndDate = row[4].String
		p.CreatedBy = row[5].Int64
		p.CreatedAt = row[6].String

		arows, aerr := storage.DB.QueryRows(
			`SELECT id, poll_id, text, display_order FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
			[]sqinn.Value{sqinn.Int64Value(p.ID)},
			[]byte{sqinn.ValInt64, sqinn.ValInt64, sqinn.ValString, sqinn.ValInt32},
		)
		if aerr != nil {
			web.RespondError(w, http.StatusInternalServerError, "db error")
			return
		}

		var answers []models.Answer
		for _, arow := range arows {
			answers = append(answers, models.Answer{
				ID:           arow[0].Int64,
				PollID:       arow[1].Int64,
				Text:         arow[2].String,
				DisplayOrder: int(arow[3].Int32),
			})
		}
		p.Answers = answers
		polls = append(polls, p)
	}

	if polls == nil {
		polls = []models.Poll{}
	}
	web.RespondJSON(w, http.StatusOK, polls)
}

// HandleGetPoll returns a single active poll with its answers.
func HandleGetPoll(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/polls/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		web.RespondError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	rows, err := storage.DB.QueryRows(
		`SELECT id, title, type, start_date, end_date, created_by, created_at FROM polls WHERE id = ?`,
		[]sqinn.Value{sqinn.Int64Value(id)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValInt64, sqinn.ValString},
	)
	if err != nil || len(rows) == 0 {
		web.RespondError(w, http.StatusNotFound, "poll not found")
		return
	}

	row := rows[0]
	var p models.Poll
	p.ID = row[0].Int64
	p.Title = row[1].String
	p.Type = row[2].String
	p.StartDate = row[3].String
	p.EndDate = row[4].String
	p.CreatedBy = row[5].Int64
	p.CreatedAt = row[6].String

	if !poll.IsActive(p.StartDate, p.EndDate) {
		web.RespondError(w, http.StatusGone, "poll is no longer active")
		return
	}

	arows, err := storage.DB.QueryRows(
		`SELECT id, poll_id, text, display_order FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
		[]sqinn.Value{sqinn.Int64Value(p.ID)},
		[]byte{sqinn.ValInt64, sqinn.ValInt64, sqinn.ValString, sqinn.ValInt32},
	)
	if err != nil {
		web.RespondError(w, http.StatusInternalServerError, "db error")
		return
	}

	var answers []models.Answer
	for _, arow := range arows {
		answers = append(answers, models.Answer{
			ID:           arow[0].Int64,
			PollID:       arow[1].Int64,
			Text:         arow[2].String,
			DisplayOrder: int(arow[3].Int32),
		})
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

	storage.DB.MustExecParams(
		`INSERT INTO polls (title, type, start_date, end_date, allow_blank, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		1, 7,
		[]sqinn.Value{
			sqinn.StringValue(req.Title),
			sqinn.StringValue(req.Type),
			sqinn.StringValue(req.StartDate),
			sqinn.StringValue(req.EndDate),
			sqinn.Int64Value(storage.BoolToInt(req.AllowBlank)),
			sqinn.Int64Value(admin.ID),
			sqinn.StringValue(now),
		},
	)

	rows, _ := storage.DB.QueryRows("SELECT id FROM polls ORDER BY id DESC LIMIT 1", nil, []byte{sqinn.ValInt64})
	if len(rows) == 0 {
		web.RespondError(w, http.StatusInternalServerError, "error retrieving poll id")
		return
	}
	lastInsertID := rows[0][0].Int64

	for i, answer := range req.Answers {
		text := strings.TrimSpace(answer.Text)
		if text == "" {
			continue
		}

		storage.DB.MustExecParams(
			`INSERT INTO answers (poll_id, text, display_order) VALUES (?, ?, ?)`,
			1, 3,
			[]sqinn.Value{
				sqinn.Int64Value(lastInsertID),
				sqinn.StringValue(text),
				sqinn.Int64Value(int64(i)),
			},
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

	prows, err := storage.DB.QueryRows(`SELECT end_date FROM polls WHERE id = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValString},
	)
	if err != nil || len(prows) == 0 {
		web.RespondError(w, http.StatusNotFound, "poll not found")
		return
	}
	pollEndDate, _ := time.Parse(time.RFC3339, prows[0][0].String)

	arows, err := storage.DB.QueryRows(
		`SELECT id, text FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValInt64, sqinn.ValString},
	)
	if err != nil {
		web.RespondError(w, http.StatusInternalServerError, "db error")
		return
	}

	answerMap := make(map[int64]models.ResultAnswer)
	for _, arow := range arows {
		id := arow[0].Int64
		text := arow[1].String
		answerMap[id] = models.ResultAnswer{ID: id, Text: text, Votes: 0}
	}

	vrows, err := storage.DB.QueryRows(
		`SELECT answer_ids FROM votes WHERE poll_id = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValString},
	)
	if err != nil {
		web.RespondError(w, http.StatusInternalServerError, "db error")
		return
	}

	for _, vrow := range vrows {
		var ids []int64
		json.Unmarshal([]byte(vrow[0].String), &ids)
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
	rows, err := storage.DB.QueryRows("SELECT count(DISTINCT voter_hash) FROM votes", nil, []byte{sqinn.ValInt64})
	if err != nil || len(rows) == 0 {
		web.RespondError(w, http.StatusInternalServerError, "db error")
		return
	}
	totalVotes := rows[0][0].Int64

	const totalEligible = 1000.0
	turnout := (float64(totalVotes) / totalEligible) * 100

	trows, _ := storage.DB.QueryRows(
		`SELECT strftime('%Y-%m-%dT%H:00:00', voted_at) as hour, count(*)
         FROM votes GROUP BY hour ORDER BY hour ASC`,
		nil, []byte{sqinn.ValString, sqinn.ValInt64},
	)

	timeline := make([]map[string]interface{}, 0, len(trows))
	for _, row := range trows {
		timeline = append(timeline, map[string]interface{}{
			"hour":  row[0].String,
			"count": row[1].Int64,
		})
	}

	web.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"total_votes": totalVotes,
		"turnout_pct": turnout,
		"timeline":    timeline,
	})
}
