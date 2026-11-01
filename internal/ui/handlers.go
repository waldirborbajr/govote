// Package ui implements the server-rendered HTMX handlers for the voter flow
// and the administrator dashboard.
package ui

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/waldirborbajr/govote/internal/models"
	"github.com/waldirborbajr/govote/internal/notify"
	"github.com/waldirborbajr/govote/internal/poll"
	"github.com/waldirborbajr/govote/internal/security"
	"github.com/waldirborbajr/govote/internal/storage"
	"github.com/waldirborbajr/govote/internal/validate"
	"github.com/waldirborbajr/govote/internal/web"
)

// HandleUIIndex renders the landing page.
func HandleUIIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.Templates.ExecuteTemplate(w, "page", web.PageData{}); err != nil {
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
}

// HandleUIVerifyForm renders the passcode verification form.
func HandleUIVerifyForm(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.Templates.ExecuteTemplate(w, "verify_form", web.PageData{}); err != nil {
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
}

// HandleUIVotingFlow renders the voter entry choices.
func HandleUIVotingFlow(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.Templates.ExecuteTemplate(w, "voting_flow", web.PageData{}); err != nil {
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
}

// HandleUIRequestPasscodeForm renders the request-passcode form.
func HandleUIRequestPasscodeForm(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.Templates.ExecuteTemplate(w, "auth", web.PageData{}); err != nil {
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
}

// HandleUIAdmin renders the admin dashboard or login form depending on session.
func HandleUIAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	admin, err := web.GetAuthenticatedAdmin(r)
	if err != nil {
		if err := web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{}); err != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}

	if err := web.Templates.ExecuteTemplate(w, "admin_dashboard", web.PageData{AdminUser: admin}); err != nil {
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
}

// HandleUIAdminPolls renders the polls managed by the authenticated admin.
func HandleUIAdminPolls(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	admin, err := web.GetAuthenticatedAdmin(r)
	if err != nil {
		if err := web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Sessão expirada."}); err != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}
	renderUIAdminPollsList(w, admin, "")
}

func renderUIAdminPollsList(w http.ResponseWriter, admin *models.Admin, msg string) {
	var rows *sql.Rows
	var err error

	if admin.IsSuper {
		rows, err = storage.DB.Query(
			`SELECT id, title, type, start_date, end_date, created_by, created_at FROM polls ORDER BY created_at DESC`,
		)
	} else {
		rows, err = storage.DB.Query(
			`SELECT id, title, type, start_date, end_date, created_by, created_at FROM polls WHERE created_by = ? ORDER BY created_at DESC`,
			admin.ID,
		)
	}

	if err != nil {
		if err := web.Templates.ExecuteTemplate(w, "polls", web.PageData{Error: "Erro ao carregar enquetes do banco."}); err != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}
	defer func() { _ = rows.Close() }()

	var polls []models.Poll
	for rows.Next() {
		var p models.Poll
		if err := rows.Scan(&p.ID, &p.Title, &p.Type, &p.StartDate, &p.EndDate, &p.CreatedBy, &p.CreatedAt); err != nil {
			continue
		}
		polls = append(polls, p)
	}

	if err := web.Templates.ExecuteTemplate(w, "polls", web.PageData{Polls: polls, AdminUser: admin, Message: msg}); err != nil {
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
}

// HandleUIRequestPasscode handles the voter passcode request form submission.
func HandleUIRequestPasscode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulário inválido", http.StatusBadRequest)
		return
	}

	cpfRaw := strings.TrimSpace(r.FormValue("cpf"))
	name := strings.TrimSpace(r.FormValue("name"))
	countryCode := strings.TrimSpace(r.FormValue("country_code"))
	phoneRaw := strings.TrimSpace(r.FormValue("phone"))

	if cpfRaw == "" || name == "" || phoneRaw == "" {
		if err := web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: "cpf, nome e telefone são obrigatórios"}); err != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}

	cpf := strings.ReplaceAll(strings.ReplaceAll(cpfRaw, ".", ""), "-", "")
	phone := countryCode + strings.ReplaceAll(strings.ReplaceAll(phoneRaw, "(", ""), ")", "")
	phone = strings.ReplaceAll(phone, "-", "")
	phone = strings.ReplaceAll(phone, " ", "")

	if err := validate.CPF(cpf); err != nil {
		if renderErr := web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: err.Error(), CSRFToken: web.EnsureCSRFToken(w, r)}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}
	if err := validate.Name(name); err != nil {
		if renderErr := web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: err.Error(), CSRFToken: web.EnsureCSRFToken(w, r)}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}
	if err := validate.Phone(phone); err != nil {
		if renderErr := web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: err.Error(), CSRFToken: web.EnsureCSRFToken(w, r)}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}

	passcode := security.GeneratePasscode()
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
		cpf,
		name,
		phone,
		security.HashPasscode(passcode),
		expiresAt,
	); err != nil {
		if renderErr := web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: "Se os dados forem válidos, um código será enviado.", CSRFToken: web.EnsureCSRFToken(w, r)}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}

	whatsappURL := notify.BuildWhatsAppURL(phone, passcode)
	storage.LogAction("PASSCODE_ISSUED", "cpf_fp="+security.TokenFingerprint(cpf))

	if err := web.Templates.ExecuteTemplate(w, "passcode_sent", web.PageData{
		WhatsAppURL: whatsappURL,
		CSRFToken:   web.EnsureCSRFToken(w, r),
	}); err != nil {
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
}

// HandleUIVerify handles the voter passcode verification form submission.
func HandleUIVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulário inválido", http.StatusBadRequest)
		return
	}
	cpf := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(r.FormValue("cpf")), ".", ""), "-", "")
	passcode := strings.TrimSpace(r.FormValue("passcode"))
	csrf := web.EnsureCSRFToken(w, r)

	if cpf == "" || passcode == "" {
		if err := web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: "cpf e passcode obrigatórios", CSRFToken: csrf}); err != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}
	if err := validate.CPF(cpf); err != nil {
		if renderErr := web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: err.Error(), CSRFToken: csrf}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}
	if err := validate.Passcode(passcode); err != nil {
		if renderErr := web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: err.Error(), CSRFToken: csrf}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}

	lockKey := storage.LockoutKeyCPF(cpf)
	if locked, remaining := storage.IsLocked(lockKey); locked {
		if err := web.Templates.ExecuteTemplate(w, "auth", web.PageData{
			Error:     "Muitas tentativas. Tente novamente em " + remaining.Round(time.Second).String() + ".",
			CSRFToken: csrf,
		}); err != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}

	const generic = "credenciais inválidas"
	var storedHash, usedAt, expiresAt sql.NullString
	err := storage.DB.QueryRow(
		`SELECT passcode, used_at, passcode_expires_at FROM voters WHERE cpf = ?`,
		cpf,
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
		security.CheckPasscode(storedHash.String, passcode) &&
		(!usedAt.Valid || usedAt.String == "") &&
		notExpired

	if !ok {
		storage.RecordFailure(lockKey)
		storage.LogActionIP("VOTER_VERIFY_FAIL", "cpf_fp="+security.TokenFingerprint(cpf), web.ClientIP(r))
		if err := web.Templates.ExecuteTemplate(w, "auth", web.PageData{Error: generic, CSRFToken: csrf}); err != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}
	storage.ClearFailures(lockKey)
	storage.LogActionIP("VOTER_VERIFY_OK", "cpf_fp="+security.TokenFingerprint(cpf), web.ClientIP(r))

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := storage.DB.Exec(
		`UPDATE voters SET passcode = NULL, passcode_expires_at = NULL, verified_at = ?, used_at = ? WHERE cpf = ?`,
		now, now, cpf,
	); err != nil {
		http.Error(w, "erro ao verificar eleitor", http.StatusInternalServerError)
		return
	}

	token := security.GenerateVoterToken(cpf)
	http.SetCookie(w, &http.Cookie{
		Name:     "voter_token",
		Value:    token,
		Path:     "/",
		MaxAge:   int(security.VoterTokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})

	renderUIVoterPolls(w, cpf, "")
}

func renderUIVoterPolls(w http.ResponseWriter, cpf, errMsg string) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := storage.DB.Query(
		`SELECT id, title, type, start_date, end_date, created_at FROM polls
		 WHERE start_date <= ? AND end_date >= ? ORDER BY created_at DESC`,
		now, now,
	)
	if err != nil {
		if renderErr := web.Templates.ExecuteTemplate(w, "polls", web.PageData{CPF: cpf, Error: "db error"}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}
	defer func() { _ = rows.Close() }()

	var polls []models.Poll
	for rows.Next() {
		var p models.Poll
		if err := rows.Scan(&p.ID, &p.Title, &p.Type, &p.StartDate, &p.EndDate, &p.CreatedAt); err != nil {
			continue
		}
		polls = append(polls, p)
	}

	if err := web.Templates.ExecuteTemplate(w, "polls", web.PageData{CPF: cpf, Polls: polls, Error: errMsg}); err != nil {
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
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
		if renderErr := web.Templates.ExecuteTemplate(w, "polls", web.PageData{CPF: cpf, Error: "invalid poll id"}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}

	var p models.Poll
	err = storage.DB.QueryRow(
		`SELECT id, title, type, start_date, end_date, created_at FROM polls WHERE id = ?`,
		id,
	).Scan(&p.ID, &p.Title, &p.Type, &p.StartDate, &p.EndDate, &p.CreatedAt)
	if err != nil {
		if renderErr := web.Templates.ExecuteTemplate(w, "polls", web.PageData{CPF: cpf, Error: "poll not found"}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}

	if !poll.IsActive(p.StartDate, p.EndDate) {
		if renderErr := web.Templates.ExecuteTemplate(w, "polls", web.PageData{CPF: cpf, Error: "poll is no longer active"}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}

	arows, err := storage.DB.Query(
		`SELECT id, poll_id, text, display_order FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
		p.ID,
	)
	if err != nil {
		if renderErr := web.Templates.ExecuteTemplate(w, "polls", web.PageData{CPF: cpf, Error: "db error"}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}
	defer func() { _ = arows.Close() }()

	var answers []models.Answer
	for arows.Next() {
		var a models.Answer
		if err := arows.Scan(&a.ID, &a.PollID, &a.Text, &a.DisplayOrder); err != nil {
			continue
		}
		answers = append(answers, a)
	}
	p.Answers = answers

	if err := web.Templates.ExecuteTemplate(w, "poll_detail", web.PageData{CPF: cpf, Poll: p}); err != nil {
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
}

// HandleUIVote handles a vote submitted from the HTMX voting form.
func HandleUIVote(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulário inválido", http.StatusBadRequest)
		return
	}

	token := ""
	if c, err := r.Cookie("voter_token"); err == nil {
		token = c.Value
	}
	tokenCPF, tokenOK := security.ValidateVoterToken(token)
	if !tokenOK {
		if err := web.Templates.ExecuteTemplate(w, "vote_result", web.PageData{
			Error:     "sessão de voto expirada — verifique o código novamente",
			CSRFToken: web.EnsureCSRFToken(w, r),
		}); err != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}
	cpf := tokenCPF

	idStr := strings.TrimPrefix(r.URL.Path, "/ui/polls/")
	idStr = strings.TrimSuffix(idStr, "/vote")
	pollID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		if renderErr := web.Templates.ExecuteTemplate(w, "vote_result", web.PageData{CPF: cpf, Error: "invalid poll id", CSRFToken: web.EnsureCSRFToken(w, r)}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}

	answerIDStrs := r.Form["answer_ids"]
	var answerIDs []int64
	for _, s := range answerIDStrs {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			if renderErr := web.Templates.ExecuteTemplate(w, "vote_result", web.PageData{CPF: cpf, Error: "invalid answer id", CSRFToken: web.EnsureCSRFToken(w, r)}); renderErr != nil {
				http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
			}
			return
		}
		answerIDs = append(answerIDs, n)
	}

	if voteErr := poll.CastVote(pollID, cpf, answerIDs); voteErr != nil {
		if renderErr := web.Templates.ExecuteTemplate(w, "vote_result", web.PageData{CPF: cpf, Error: voteErr.Message, CSRFToken: web.EnsureCSRFToken(w, r)}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}

	if err := web.Templates.ExecuteTemplate(w, "vote_result", web.PageData{CPF: cpf, CSRFToken: web.EnsureCSRFToken(w, r)}); err != nil {
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
}

// HandleUIGlobalStats renders the global statistics dashboard fragment.
func HandleUIGlobalStats(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.Templates.ExecuteTemplate(w, "global_stats", web.PageData{}); err != nil {
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
}

// HandleUICreatePollForm renders the poll creation form for an authenticated admin.
func HandleUICreatePollForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	admin, err := web.GetAuthenticatedAdmin(r)
	if err != nil {
		if renderErr := web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Faça login para continuar"}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}
	if err := web.Templates.ExecuteTemplate(w, "create_poll", web.PageData{AdminUser: admin}); err != nil {
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
}

// HandleUICreatePoll persists a poll submitted from the HTMX creation form.
func HandleUICreatePoll(w http.ResponseWriter, r *http.Request) {
	admin, err := web.GetAuthenticatedAdmin(r)
	if err != nil {
		web.RespondError(w, http.StatusUnauthorized, "Sessão expirada ou não autenticada.")
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulário inválido", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	pType := r.FormValue("type")
	startDate := r.FormValue("start_date")
	endDate := r.FormValue("end_date")
	allowBlank := r.FormValue("allow_blank") == "true"
	answersRaw := strings.Split(r.FormValue("answers"), "\n")

	if err := validate.PollTitle(title); err != nil {
		web.RespondError(w, http.StatusBadRequest, "dados inválidos")
		return
	}
	if pType != "radio" && pType != "checkbox" {
		web.RespondError(w, http.StatusBadRequest, "dados inválidos")
		return
	}
	if _, err := time.Parse(time.RFC3339, startDate); err != nil {
		// accept datetime-local without Z by appending :00Z if needed — still generic error
		if _, err2 := time.Parse("2006-01-02T15:04", startDate); err2 != nil {
			web.RespondError(w, http.StatusBadRequest, "dados inválidos")
			return
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)

	res, err := storage.DB.Exec(
		`INSERT INTO polls (title, type, start_date, end_date, allow_blank, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		title,
		pType,
		startDate,
		endDate,
		storage.BoolToInt(allowBlank),
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

	for i, text := range answersRaw {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if _, err := storage.DB.Exec(
			`INSERT INTO answers (poll_id, text, display_order) VALUES (?, ?, ?)`,
			lastInsertID,
			text,
			i,
		); err != nil {
			web.RespondError(w, http.StatusInternalServerError, "error creating poll answer")
			return
		}
	}

	renderUIAdminPollsList(w, admin, "Enquete publicada com sucesso!")
}

// HandleUIResults renders a poll's results for the owning admin (or super admin).
func HandleUIResults(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	admin, err := web.GetAuthenticatedAdmin(r)
	if err != nil {
		if renderErr := web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Acesso restrito."}); renderErr != nil {
			http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
		}
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/ui/polls/")
	idStr = strings.TrimSuffix(idStr, "/results")
	pollID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		renderUIAdminPollsList(w, admin, "ID Inválido")
		return
	}

	var p models.Poll
	err = storage.DB.QueryRow(`SELECT id, title, type, start_date, end_date, created_by FROM polls WHERE id = ?`, pollID).
		Scan(&p.ID, &p.Title, &p.Type, &p.StartDate, &p.EndDate, &p.CreatedBy)
	if err != nil {
		renderUIAdminPollsList(w, admin, "Enquete não encontrada.")
		return
	}

	if !admin.IsSuper && admin.ID != p.CreatedBy {
		renderUIAdminPollsList(w, admin, "Acesso negado: Você só pode ver os resultados das suas próprias enquetes.")
		return
	}

	results, err := poll.GetResults(pollID)
	if err != nil {
		renderUIAdminPollsList(w, admin, "Erro na leitura de resultados")
		return
	}

	if err := web.Templates.ExecuteTemplate(w, "results", web.PageData{AdminUser: admin, Poll: p, Results: results}); err != nil {
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
}
