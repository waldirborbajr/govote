// Package ui implements the server-rendered HTMX handlers for the voter flow
// and the administrator dashboard.
package ui

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

// HandleUIIndex renders the landing page.
func HandleUIIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	web.Templates.ExecuteTemplate(w, "page", web.PageData{})
}

// HandleUIVerifyForm renders the passcode verification form.
func HandleUIVerifyForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	web.Templates.ExecuteTemplate(w, "verify_form", web.PageData{})
}

// HandleUIVotingFlow renders the voter entry choices.
func HandleUIVotingFlow(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	web.Templates.ExecuteTemplate(w, "voting_flow", web.PageData{})
}

// HandleUIRequestPasscodeForm renders the request-passcode form.
func HandleUIRequestPasscodeForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	web.Templates.ExecuteTemplate(w, "auth", web.PageData{})
}

// HandleUIAdmin renders the admin dashboard or login form depending on session.
func HandleUIAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	admin, err := web.GetAuthenticatedAdmin(r)
	if err != nil {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{})
		return
	}

	web.Templates.ExecuteTemplate(w, "admin_dashboard", web.PageData{AdminUser: admin})
}

// HandleUIAdminPolls renders the polls managed by the authenticated admin.
func HandleUIAdminPolls(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	admin, err := web.GetAuthenticatedAdmin(r)
	if err != nil {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Sessão expirada."})
		return
	}
	renderUIAdminPollsList(w, admin, "")
}

func renderUIAdminPollsList(w http.ResponseWriter, admin *models.Admin, msg string) {
	var rows [][]sqinn.Value
	var err error

	if admin.IsSuper {
		rows, err = storage.DB.QueryRows(
			`SELECT id, title, type, start_date, end_date, created_by, created_at FROM polls ORDER BY created_at DESC`,
			nil, []byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValInt64, sqinn.ValString},
		)
	} else {
		rows, err = storage.DB.QueryRows(
			`SELECT id, title, type, start_date, end_date, created_by, created_at FROM polls WHERE created_by = ? ORDER BY created_at DESC`,
			[]sqinn.Value{sqinn.Int64Value(admin.ID)},
			[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValInt64, sqinn.ValString},
		)
	}

	if err != nil {
		web.Templates.ExecuteTemplate(w, "polls", web.PageData{Error: "Erro ao carregar enquetes do banco."})
		return
	}

	var polls []models.Poll
	for _, row := range rows {
		polls = append(polls, models.Poll{
			ID:        row[0].Int64,
			Title:     row[1].String,
			Type:      row[2].String,
			StartDate: row[3].String,
			EndDate:   row[4].String,
			CreatedBy: row[5].Int64,
			CreatedAt: row[6].String,
		})
	}

	web.Templates.ExecuteTemplate(w, "polls", web.PageData{Polls: polls, AdminUser: admin, Message: msg})
}

// HandleUIRequestPasscode handles the voter passcode request form submission.
func HandleUIRequestPasscode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()

	cpfRaw := strings.TrimSpace(r.FormValue("cpf"))
	name := strings.TrimSpace(r.FormValue("name"))
	countryCode := strings.TrimSpace(r.FormValue("country_code"))
	phoneRaw := strings.TrimSpace(r.FormValue("phone"))

	if cpfRaw == "" || name == "" || phoneRaw == "" {
		web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: "cpf, nome e telefone são obrigatórios"})
		return
	}

	cpf := strings.ReplaceAll(strings.ReplaceAll(cpfRaw, ".", ""), "-", "")
	phone := countryCode + strings.ReplaceAll(strings.ReplaceAll(phoneRaw, "(", ""), ")", "")
	phone = strings.ReplaceAll(phone, "-", "")
	phone = strings.ReplaceAll(phone, " ", "")

	if len(cpf) != 11 {
		web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: "CPF inválido"})
		return
	}

	passcode := security.GeneratePasscode()

	storage.DB.MustExecParams(
		`INSERT INTO voters (cpf, name, phone, passcode, verified_at)
		 VALUES (?, ?, ?, ?, NULL)
		 ON CONFLICT(cpf) DO UPDATE SET passcode=excluded.passcode`,
		1, 4,
		[]sqinn.Value{
			sqinn.StringValue(cpf),
			sqinn.StringValue(name),
			sqinn.StringValue(phone),
			sqinn.StringValue(security.HashPasscode(passcode)),
		},
	)

	whatsappURL := notify.BuildWhatsAppURL(phone, passcode)
	fmt.Printf("[PoC] CPF %s | Phone %s | Passcode %s\n", cpf, phone, passcode)

	web.Templates.ExecuteTemplate(w, "passcode_sent", web.PageData{WhatsAppURL: whatsappURL})
}

// HandleUIVerify handles the voter passcode verification form submission.
func HandleUIVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()
	cpf := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(r.FormValue("cpf")), ".", ""), "-", "")
	passcode := strings.TrimSpace(r.FormValue("passcode"))

	if cpf == "" || passcode == "" {
		web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: "cpf e passcode obrigatórios"})
		return
	}

	rows, err := storage.DB.QueryRows(`SELECT passcode, used_at FROM voters WHERE cpf = ?`,
		[]sqinn.Value{sqinn.StringValue(cpf)},
		[]byte{sqinn.ValString, sqinn.ValString},
	)
	if err != nil || len(rows) == 0 {
		web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: "cpf não encontrado"})
		return
	}

	if rows[0][0].String == "" || !security.CheckPasscode(rows[0][0].String, passcode) {
		web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: "código incorreto"})
		return
	}

	if rows[0][1].String != "" {
		web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: "Este código já foi utilizado. Solicite um novo."})
		return
	}

	storage.DB.MustExecParams(
		`UPDATE voters SET passcode = NULL, used_at = ? WHERE cpf = ?`,
		1, 2,
		[]sqinn.Value{
			sqinn.StringValue(time.Now().UTC().Format(time.RFC3339)),
			sqinn.StringValue(cpf),
		},
	)

	renderUIVoterPolls(w, cpf, "")
}

func renderUIVoterPolls(w http.ResponseWriter, cpf, errMsg string) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := storage.DB.QueryRows(
		`SELECT id, title, type, start_date, end_date, created_at FROM polls
		 WHERE start_date <= ? AND end_date >= ? ORDER BY created_at DESC`,
		[]sqinn.Value{sqinn.StringValue(now), sqinn.StringValue(now)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString},
	)
	if err != nil {
		web.Templates.ExecuteTemplate(w, "polls", web.PageData{CPF: cpf, Error: "db error"})
		return
	}

	var polls []models.Poll
	for _, row := range rows {
		polls = append(polls, models.Poll{
			ID:        row[0].Int64,
			Title:     row[1].String,
			Type:      row[2].String,
			StartDate: row[3].String,
			EndDate:   row[4].String,
			CreatedAt: row[5].String,
		})
	}

	web.Templates.ExecuteTemplate(w, "polls", web.PageData{CPF: cpf, Polls: polls, Error: errMsg})
}

// HandleUIPolls renders the active polls for a voter identified by cpf query.
func HandleUIPolls(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	renderUIVoterPolls(w, r.URL.Query().Get("cpf"), "")
}

// HandleUIPollDetail renders the voting form for a single active poll.
func HandleUIPollDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cpf := r.URL.Query().Get("cpf")

	idStr := strings.TrimPrefix(r.URL.Path, "/ui/polls/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		web.Templates.ExecuteTemplate(w, "polls", web.PageData{CPF: cpf, Error: "invalid poll id"})
		return
	}

	rows, err := storage.DB.QueryRows(
		`SELECT id, title, type, start_date, end_date, created_at FROM polls WHERE id = ?`,
		[]sqinn.Value{sqinn.Int64Value(id)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString},
	)
	if err != nil || len(rows) == 0 {
		web.Templates.ExecuteTemplate(w, "polls", web.PageData{CPF: cpf, Error: "poll not found"})
		return
	}

	row := rows[0]
	var p models.Poll
	p.ID = row[0].Int64
	p.Title = row[1].String
	p.Type = row[2].String
	p.StartDate = row[3].String
	p.EndDate = row[4].String
	p.CreatedAt = row[5].String

	if !poll.IsActive(p.StartDate, p.EndDate) {
		web.Templates.ExecuteTemplate(w, "polls", web.PageData{CPF: cpf, Error: "poll is no longer active"})
		return
	}

	arows, err := storage.DB.QueryRows(
		`SELECT id, poll_id, text, display_order FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
		[]sqinn.Value{sqinn.Int64Value(p.ID)},
		[]byte{sqinn.ValInt64, sqinn.ValInt64, sqinn.ValString, sqinn.ValInt32},
	)
	if err != nil {
		web.Templates.ExecuteTemplate(w, "polls", web.PageData{CPF: cpf, Error: "db error"})
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

	web.Templates.ExecuteTemplate(w, "poll_detail", web.PageData{CPF: cpf, Poll: p})
}

// HandleUIVote handles a vote submitted from the HTMX voting form.
func HandleUIVote(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()
	cpf := strings.TrimSpace(r.FormValue("cpf"))

	idStr := strings.TrimPrefix(r.URL.Path, "/ui/polls/")
	idStr = strings.TrimSuffix(idStr, "/vote")
	pollID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		web.Templates.ExecuteTemplate(w, "vote_result", web.PageData{CPF: cpf, Error: "invalid poll id"})
		return
	}

	answerIDStrs := r.Form["answer_ids"]
	var answerIDs []int64
	for _, s := range answerIDStrs {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			web.Templates.ExecuteTemplate(w, "vote_result", web.PageData{CPF: cpf, Error: "invalid answer id"})
			return
		}
		answerIDs = append(answerIDs, n)
	}

	if voteErr := poll.CastVote(pollID, cpf, answerIDs); voteErr != nil {
		web.Templates.ExecuteTemplate(w, "vote_result", web.PageData{CPF: cpf, Error: voteErr.Message})
		return
	}

	web.Templates.ExecuteTemplate(w, "vote_result", web.PageData{CPF: cpf})
}

// HandleUIGlobalStats renders the global statistics dashboard fragment.
func HandleUIGlobalStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	web.Templates.ExecuteTemplate(w, "global_stats", web.PageData{})
}

// HandleUICreatePollForm renders the poll creation form for an authenticated admin.
func HandleUICreatePollForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	admin, err := web.GetAuthenticatedAdmin(r)
	if err != nil {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Faça login para continuar"})
		return
	}
	web.Templates.ExecuteTemplate(w, "create_poll", web.PageData{AdminUser: admin})
}

// HandleUICreatePoll persists a poll submitted from the HTMX creation form.
func HandleUICreatePoll(w http.ResponseWriter, r *http.Request) {
	admin, err := web.GetAuthenticatedAdmin(r)
	if err != nil {
		web.RespondError(w, http.StatusUnauthorized, "Sessão expirada ou não autenticada.")
		return
	}

	r.ParseForm()

	title := r.FormValue("title")
	pType := r.FormValue("type")
	startDate := r.FormValue("start_date")
	endDate := r.FormValue("end_date")
	allowBlank := r.FormValue("allow_blank") == "true"
	answersRaw := strings.Split(r.FormValue("answers"), "\n")
	now := time.Now().UTC().Format(time.RFC3339)

	storage.DB.MustExecParams(
		`INSERT INTO polls (title, type, start_date, end_date, allow_blank, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		1, 7,
		[]sqinn.Value{
			sqinn.StringValue(title),
			sqinn.StringValue(pType),
			sqinn.StringValue(startDate),
			sqinn.StringValue(endDate),
			sqinn.Int64Value(storage.BoolToInt(allowBlank)),
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

	for i, text := range answersRaw {
		text = strings.TrimSpace(text)
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

	renderUIAdminPollsList(w, admin, "Enquete publicada com sucesso!")
}

// HandleUIResults renders a poll's results for the owning admin (or super admin).
func HandleUIResults(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	admin, err := web.GetAuthenticatedAdmin(r)
	if err != nil {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Acesso restrito."})
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/ui/polls/")
	idStr = strings.TrimSuffix(idStr, "/results")
	pollID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		renderUIAdminPollsList(w, admin, "ID Inválido")
		return
	}

	prows, err := storage.DB.QueryRows(`SELECT id, title, type, start_date, end_date, created_by FROM polls WHERE id = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValInt64},
	)
	if err != nil || len(prows) == 0 {
		renderUIAdminPollsList(w, admin, "Enquete não encontrada.")
		return
	}

	createdBy := prows[0][5].Int64
	if !admin.IsSuper && admin.ID != createdBy {
		renderUIAdminPollsList(w, admin, "Acesso negado: Você só pode ver os resultados das suas próprias enquetes.")
		return
	}

	row := prows[0]
	var p models.Poll
	p.ID = row[0].Int64
	p.Title = row[1].String
	p.Type = row[2].String
	p.StartDate = row[3].String
	p.EndDate = row[4].String
	p.CreatedBy = row[5].Int64

	arows, err := storage.DB.QueryRows(`SELECT id, text FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValInt64, sqinn.ValString},
	)
	if err != nil {
		renderUIAdminPollsList(w, admin, "Erro na leitura de respostas")
		return
	}

	answerMap := make(map[int64]*models.ResultAnswer)
	var order []int64
	for _, arow := range arows {
		id := arow[0].Int64
		text := arow[1].String
		answerMap[id] = &models.ResultAnswer{ID: id, Text: text, Votes: 0}
		order = append(order, id)
	}

	vrows, err := storage.DB.QueryRows(`SELECT answer_ids FROM votes WHERE poll_id = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValString},
	)

	for _, vrow := range vrows {
		var ids []int64
		json.Unmarshal([]byte(vrow[0].String), &ids)
		for _, id := range ids {
			if a, ok := answerMap[id]; ok {
				a.Votes++
			}
		}
	}

	var results []models.ResultAnswer
	for _, id := range order {
		if a, ok := answerMap[id]; ok {
			results = append(results, *a)
		}
	}

	web.Templates.ExecuteTemplate(w, "results", web.PageData{AdminUser: admin, Poll: p, Results: results})
}
