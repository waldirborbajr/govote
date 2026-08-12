// api/handlers.go
// Package api implements the JSON HTTP handlers for the public voting API and
// the admin statistics endpoint.
package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/waldirborbajr/govote/internal/cache"
	"github.com/waldirborbajr/govote/internal/models"
	"github.com/waldirborbajr/govote/internal/notify"
	"github.com/waldirborbajr/govote/internal/poll"
	"github.com/waldirborbajr/govote/internal/security"
	"github.com/waldirborbajr/govote/internal/storage"
	"github.com/waldirborbajr/govote/internal/validate"
	"github.com/waldirborbajr/govote/internal/web"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// decodeJSON limita o tamanho do body, rejeita campos desconhecidos e
// garante que o JSON está bem formado (sem conteúdo extra após o objeto).
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if err == io.EOF {
			web.RespondError(w, http.StatusBadRequest, "body vazio")
			return false
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			web.RespondError(w, http.StatusRequestEntityTooLarge, "payload muito grande")
			return false
		}
		web.RespondError(w, http.StatusBadRequest, "json inválido")
		return false
	}
	// garante que não há lixo extra após o objeto principal
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		web.RespondError(w, http.StatusBadRequest, "json inválido (conteúdo extra)")
		return false
	}
	return true
}

// HandleRequestPasscode generates and stores a voter passcode and returns a
// WhatsApp deep link to deliver it.
func HandleRequestPasscode(w http.ResponseWriter, r *http.Request) {
	var req models.RequestPasscodeReq
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := validate.CPF(req.CPF); err != nil {
		web.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validate.Name(req.Name); err != nil {
		web.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validate.Phone(req.Phone); err != nil {
		web.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// normaliza antes de gravar
	req.CPF = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(req.CPF), ".", ""), "-", "")
	req.Name = strings.TrimSpace(req.Name)
	req.Phone = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(req.Phone), "(", ""), ")", ""), "-", ""), " ", "")

	passcode := security.GeneratePasscode()

	whatsappURL := ""
	expiresAt := time.Now().UTC().Add(security.PasscodeTTL).Format(time.RFC3339)
	if _, err := storage.DB.Exec(
		`INSERT INTO voters (cpf, name, phone, passcode, passcode_expires_at, verified_at, used_at)
		 VALUES (?, ?, ?, ?, ?, NULL, NULL)
		 ON CONFLICT(cpf) DO UPDATE SET
		   passcode=excluded.passcode,
		   passcode_expires_at=excluded.passcode_expires_at,
		   name=excluded.name,
		   phone=excluded.phone,
		   verified_at=NULL,
		   used_at=NULL`,
		req.CPF,
		req.Name,
		req.Phone,
		security.HashPasscode(passcode),
		expiresAt,
	); err == nil {
		whatsappURL = notify.BuildWhatsAppURL(req.Phone, passcode)
		storage.LogAction("PASSCODE_ISSUED", "cpf_fp="+security.TokenFingerprint(req.CPF))
	}
	// Nunca logar o passcode. Resposta uniforme (anti-enumeração de erros internos).
	web.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"message":      "Se os dados forem válidos, um código será enviado.",
		"whatsapp_url": whatsappURL,
	})
}

// HandleVerify validates a voter's passcode and marks it as used.
func HandleVerify(w http.ResponseWriter, r *http.Request) {
	var req models.VerifyReq
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := validate.CPF(req.CPF); err != nil {
		web.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validate.Passcode(req.Passcode); err != nil {
		web.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	req.CPF = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(req.CPF), ".", ""), "-", "")
	req.Passcode = strings.TrimSpace(req.Passcode)

	lockKey := storage.LockoutKeyCPF(req.CPF)
	if locked, _ := storage.IsLocked(lockKey); locked {
		web.RespondError(w, http.StatusTooManyRequests, "muitas tentativas — tente novamente mais tarde")
		return
	}

	const genericAuthErr = "credenciais inválidas"

	var storedHash, usedAt, expiresAt sql.NullString
	err := storage.DB.QueryRow(
		`SELECT passcode, used_at, passcode_expires_at FROM voters WHERE cpf = ?`,
		req.CPF,
	).Scan(&storedHash, &usedAt, &expiresAt)

	notExpired := true
	if expiresAt.Valid && expiresAt.String != "" {
		if exp, e := time.Parse(time.RFC3339, expiresAt.String); e == nil {
			notExpired = time.Now().UTC().Before(exp)
		} else {
			notExpired = false
		}
	}

	ok := err == nil &&
		storedHash.Valid && storedHash.String != "" &&
		security.CheckPasscode(storedHash.String, req.Passcode) &&
		!(usedAt.Valid && usedAt.String != "") &&
		notExpired

	if !ok {
		storage.RecordFailure(lockKey)
		storage.LogActionIP("VOTER_VERIFY_FAIL", "cpf_fp="+security.TokenFingerprint(req.CPF), web.ClientIP(r))
		web.RespondError(w, http.StatusUnauthorized, genericAuthErr)
		return
	}
	storage.ClearFailures(lockKey)
	storage.LogActionIP("VOTER_VERIFY_OK", "cpf_fp="+security.TokenFingerprint(req.CPF), web.ClientIP(r))

	now := time.Now().UTC().Format(time.RFC3339)
	storage.DB.Exec(
		`UPDATE voters SET passcode = NULL, passcode_expires_at = NULL, verified_at = ?, used_at = ? WHERE cpf = ?`,
		now,
		now,
		req.CPF,
	)

	token := security.GenerateVoterToken(req.CPF)
	http.SetCookie(w, &http.Cookie{
		Name:     "voter_token",
		Value:    token,
		Path:     "/",
		MaxAge:   int(security.VoterTokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})

	web.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"verified":   true,
		"cpf":        req.CPF,
		"vote_token": token,
	})
}

// HandleListPolls returns the currently active polls with their answers.
func HandleListPolls(w http.ResponseWriter, r *http.Request) {
	var polls []models.Poll
	if cache.GetJSON("polls:active", &polls) {
		web.RespondJSON(w, http.StatusOK, polls)
		return
	}

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
	cache.SetJSON("polls:active", polls, 15*time.Second)
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
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := validate.PollTitle(req.Title); err != nil {
		web.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validate.PollType(req.Type); err != nil {
		web.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	start, err := validate.RFC3339Date(req.StartDate, "start_date")
	if err != nil {
		web.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	end, err := validate.RFC3339Date(req.EndDate, "end_date")
	if err != nil {
		web.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !end.After(start) {
		web.RespondError(w, http.StatusBadRequest, "end_date deve ser posterior a start_date")
		return
	}

	texts := make([]string, 0, len(req.Answers))
	for _, a := range req.Answers {
		texts = append(texts, a.Text)
	}
	if err := validate.AnswerTexts(texts); err != nil {
		web.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	req.Title = strings.TrimSpace(req.Title)

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
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := validate.CPF(req.CPF); err != nil {
		web.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.CPF = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(req.CPF), ".", ""), "-", "")

	if len(req.AnswerIDs) == 0 {
		web.RespondError(w, http.StatusBadRequest, "answer_ids required")
		return
	}
	if len(req.AnswerIDs) > 20 {
		web.RespondError(w, http.StatusBadRequest, "máximo de 20 respostas")
		return
	}

	token := req.VoteToken
	if token == "" {
		if c, err := r.Cookie("voter_token"); err == nil {
			token = c.Value
		}
	}
	tokenCPF, tokenOK := security.ValidateVoterToken(token)
	if !tokenOK || tokenCPF != req.CPF {
		web.RespondError(w, http.StatusUnauthorized, "voto requer autenticação (verifique o código primeiro)")
		return
	}

	voterHash := security.HashCPF(req.CPF)
	if cache.HasVoted(pollID, voterHash) {
		web.RespondError(w, http.StatusConflict, "cpf already voted")
		return
	}

	// Async path: enqueue to Redis Stream; dedicated worker writes to SQLite.
	if cache.AsyncVotesEnabled() {
		ok := cache.EnqueueVote(cache.VoteJob{
			PollID:    pollID,
			CPF:       req.CPF,
			VoterHash: voterHash,
			AnswerIDs: req.AnswerIDs,
		})
		if !ok {
			web.RespondError(w, http.StatusServiceUnavailable, "fila de votos indisponível")
			return
		}
		// Optimistic mark to reduce duplicate enqueues; worker is source of truth.
		cache.MarkVoted(pollID, voterHash)
		web.RespondJSON(w, http.StatusAccepted, map[string]any{"voted": true, "async": true})
		return
	}

	if voteErr := poll.CastVote(pollID, req.CPF, req.AnswerIDs); voteErr != nil {
		web.RespondError(w, voteErr.Status, voteErr.Message)
		return
	}
	cache.MarkVoted(pollID, voterHash)
	cache.InvalidatePoll(pollID)
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

	cacheKey := "results:" + strconv.FormatInt(pollID, 10)
	var cached map[string]any
	if cache.GetJSON(cacheKey, &cached) {
		web.RespondJSON(w, http.StatusOK, cached)
		return
	}

	var endDateStr string
	if err := storage.DB.QueryRow(`SELECT end_date FROM polls WHERE id = ?`, pollID).Scan(&endDateStr); err != nil {
		web.RespondError(w, http.StatusNotFound, "poll not found")
		return
	}
	pollEndDate, _ := time.Parse(time.RFC3339, endDateStr)

	results, err := poll.GetResults(pollID)
	if err != nil {
		web.RespondError(w, http.StatusInternalServerError, "db error")
		return
	}

	if time.Now().After(pollEndDate) {
		notify.SimulateNotification(pollID, results)
	}

	payload := map[string]interface{}{
		"poll_id": pollID,
		"answers": results,
	}
	cache.SetJSON(cacheKey, payload, 3*time.Second)
	web.RespondJSON(w, http.StatusOK, payload)
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
