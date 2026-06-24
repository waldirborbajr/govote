package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	sqinn "github.com/cvilsmeier/sqinn-go/v2"
)

// ============================================================================
// TYPES
// ============================================================================

type Poll struct {
	ID        int64    `json:"id"`
	Title     string   `json:"title"`
	Type      string   `json:"type"`
	StartDate string   `json:"start_date"`
	EndDate   string   `json:"end_date"`
	Answers   []Answer `json:"answers"`
	CreatedAt string   `json:"created_at"`
}

type Answer struct {
	ID           int64  `json:"id"`
	PollID       int64  `json:"poll_id"`
	Text         string `json:"text"`
	DisplayOrder int    `json:"display_order"`
}

type ResultAnswer struct {
	ID    int64  `json:"id"`
	Text  string `json:"text"`
	Votes int    `json:"votes"`
}

type RequestPasscodeReq struct {
	CPF   string `json:"cpf"`
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

type VerifyReq struct {
	CPF      string `json:"cpf"`
	Passcode string `json:"passcode"`
}

type CreatePollReq struct {
	Title     string `json:"title"`
	Type      string `json:"type"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Answers   []struct {
		Text string `json:"text"`
	} `json:"answers"`
}

type VoteReq struct {
	CPF       string  `json:"cpf"`
	AnswerIDs []int64 `json:"answer_ids"`
}

// ============================================================================
// GLOBAL DB & CONFIG
// ============================================================================

var db *sqinn.Sqinn

// Salt for hash anonymization (In production, use environment variables)
const hashSalt = "super-secret-salt-value"

// ============================================================================
// SECURITY & HELPERS
// ============================================================================

// hashCPF generates a SHA-256 hash of the CPF for voter anonymization.
func hashCPF(cpf string) string {
	h := sha256.New()
	h.Write([]byte(cpf + hashSalt))
	return hex.EncodeToString(h.Sum(nil))
}

// logAction inserts an audit trail record into the database.
func logAction(action, details string) {
	now := time.Now().UTC().Format(time.RFC3339)
	db.MustExecParams(
		`INSERT INTO audit_logs (action, details, created_at) VALUES (?, ?, ?)`,
		1, 3,
		[]sqinn.Value{
			sqinn.StringValue(action),
			sqinn.StringValue(details),
			sqinn.StringValue(now),
		},
	)
}

// ============================================================================
// SCHEMA
// ============================================================================

func initDB() error {
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS voters (
			cpf TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			phone TEXT NOT NULL,
			passcode TEXT NOT NULL,
			verified_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS polls (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			type TEXT NOT NULL,
			start_date TEXT NOT NULL,
			end_date TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS answers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			poll_id INTEGER NOT NULL,
			text TEXT NOT NULL,
			display_order INTEGER NOT NULL,
			FOREIGN KEY (poll_id) REFERENCES polls(id)
		)`,
		`CREATE TABLE IF NOT EXISTS votes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			poll_id INTEGER NOT NULL,
			voter_hash TEXT NOT NULL, 
			answer_ids TEXT NOT NULL,
			voted_at TEXT NOT NULL,
			UNIQUE(poll_id, voter_hash),
			FOREIGN KEY (poll_id) REFERENCES polls(id)
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			action TEXT NOT NULL,
			details TEXT,
			created_at TEXT NOT NULL
		)`,
	}

	for _, schema := range schemas {
		db.MustExecSql(schema)
	}
	return nil
}

// ============================================================================
// HELPERS
// ============================================================================

func generatePasscode() string {
	b := make([]byte, 2)
	_, err := rand.Read(b)
	if err != nil {
		log.Fatalf("rand failed: %v", err)
	}
	val := (int(b[0])<<8 | int(b[1])) % 10000
	return fmt.Sprintf("%04d", val)
}

func buildWhatsAppURL(phone, passcode string) string {
	text := fmt.Sprintf("Your voting passcode is: %s\n\nDo not share this code with anyone.", passcode)
	encodedText := url.QueryEscape(text)
	return fmt.Sprintf("https://wa.me/%s?text=%s", phone, encodedText)
}

func isPollActive(startDate, endDate string) bool {
	now := time.Now().UTC()
	start, err1 := time.Parse(time.RFC3339, startDate)
	end, err2 := time.Parse(time.RFC3339, endDate)
	if err1 != nil || err2 != nil {
		return false
	}
	return now.After(start) && now.Before(end)
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

// ============================================================================
// HANDLERS
// ============================================================================

// POST /auth/request-passcode
func handleRequestPasscode(w http.ResponseWriter, r *http.Request) {
	var req RequestPasscodeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if strings.TrimSpace(req.CPF) == "" || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Phone) == "" {
		respondError(w, http.StatusBadRequest, "cpf, name, phone required")
		return
	}

	passcode := generatePasscode()

	db.MustExecParams(
		`INSERT INTO voters (cpf, name, phone, passcode, verified_at) 
		 VALUES (?, ?, ?, ?, NULL)
		 ON CONFLICT(cpf) DO UPDATE SET passcode=excluded.passcode`,
		1, 4,
		[]sqinn.Value{
			sqinn.StringValue(req.CPF),
			sqinn.StringValue(req.Name),
			sqinn.StringValue(req.Phone),
			sqinn.StringValue(passcode),
		},
	)

	whatsappURL := buildWhatsAppURL(req.Phone, passcode)

	fmt.Printf("[PoC] CPF %s passcode: %s (for phone %s)\n", req.CPF, passcode, req.Phone)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "passcode_generated",
		"passcode":     passcode, // only for testing
		"whatsapp_url": whatsappURL,
		"message":      "Click the link to send via WhatsApp",
	})
}

// POST /auth/verify
func handleVerify(w http.ResponseWriter, r *http.Request) {
	var req VerifyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if strings.TrimSpace(req.CPF) == "" || strings.TrimSpace(req.Passcode) == "" {
		respondError(w, http.StatusBadRequest, "cpf and passcode required")
		return
	}

	rows, err := db.QueryRows(
		`SELECT passcode FROM voters WHERE cpf = ?`,
		[]sqinn.Value{sqinn.StringValue(req.CPF)},
		[]byte{sqinn.ValString},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}
	if len(rows) == 0 {
		respondError(w, http.StatusUnauthorized, "cpf not found")
		return
	}

	storedPasscode := rows[0][0].String
	if storedPasscode != req.Passcode {
		respondError(w, http.StatusUnauthorized, "wrong passcode")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	db.MustExecParams(
		`UPDATE voters SET verified_at = ? WHERE cpf = ?`,
		1, 2,
		[]sqinn.Value{sqinn.StringValue(now), sqinn.StringValue(req.CPF)},
	)

	respondJSON(w, http.StatusOK, map[string]interface{}{"verified": true, "cpf": req.CPF})
}

// GET /polls
func handleListPolls(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339)

	rows, err := db.QueryRows(
		`SELECT id, title, type, start_date, end_date, created_at 
		 FROM polls 
		 WHERE start_date <= ? AND end_date >= ?
		 ORDER BY created_at DESC`,
		[]sqinn.Value{sqinn.StringValue(now), sqinn.StringValue(now)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}

	var polls []Poll
	for _, row := range rows {
		var p Poll
		p.ID = row[0].Int64
		p.Title = row[1].String
		p.Type = row[2].String
		p.StartDate = row[3].String
		p.EndDate = row[4].String
		p.CreatedAt = row[5].String

		arows, aerr := db.QueryRows(
			`SELECT id, poll_id, text, display_order FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
			[]sqinn.Value{sqinn.Int64Value(p.ID)},
			[]byte{sqinn.ValInt64, sqinn.ValInt64, sqinn.ValString, sqinn.ValInt32},
		)
		if aerr != nil {
			respondError(w, http.StatusInternalServerError, "db error")
			return
		}

		var answers []Answer
		for _, arow := range arows {
			answers = append(answers, Answer{
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
		polls = []Poll{}
	}
	respondJSON(w, http.StatusOK, polls)
}

// GET /polls/:id
func handleGetPoll(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/polls/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	rows, err := db.QueryRows(
		`SELECT id, title, type, start_date, end_date, created_at FROM polls WHERE id = ?`,
		[]sqinn.Value{sqinn.Int64Value(id)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString},
	)
	if err != nil || len(rows) == 0 {
		respondError(w, http.StatusNotFound, "poll not found")
		return
	}

	row := rows[0]
	var p Poll
	p.ID = row[0].Int64
	p.Title = row[1].String
	p.Type = row[2].String
	p.StartDate = row[3].String
	p.EndDate = row[4].String
	p.CreatedAt = row[5].String

	if !isPollActive(p.StartDate, p.EndDate) {
		respondError(w, http.StatusGone, "poll is no longer active")
		return
	}

	arows, err := db.QueryRows(
		`SELECT id, poll_id, text, display_order FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
		[]sqinn.Value{sqinn.Int64Value(p.ID)},
		[]byte{sqinn.ValInt64, sqinn.ValInt64, sqinn.ValString, sqinn.ValInt32},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}

	var answers []Answer
	for _, arow := range arows {
		answers = append(answers, Answer{
			ID:           arow[0].Int64,
			PollID:       arow[1].Int64,
			Text:         arow[2].String,
			DisplayOrder: int(arow[3].Int32),
		})
	}
	p.Answers = answers
	respondJSON(w, http.StatusOK, p)
}

// POST /polls
func handleCreatePoll(w http.ResponseWriter, r *http.Request) {
	var req CreatePollReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if strings.TrimSpace(req.Title) == "" || (req.Type != "radio" && req.Type != "checkbox") || len(req.Answers) == 0 {
		respondError(w, http.StatusBadRequest, "title, type (radio/checkbox), and answers required")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	db.MustExecParams(
		`INSERT INTO polls (title, type, start_date, end_date, created_at) VALUES (?, ?, ?, ?, ?)`,
		1, 5,
		[]sqinn.Value{
			sqinn.StringValue(req.Title),
			sqinn.StringValue(req.Type),
			sqinn.StringValue(req.StartDate),
			sqinn.StringValue(req.EndDate),
			sqinn.StringValue(now),
		},
	)

	// Return latest poll as fallback
	handleListPolls(w, r)
}

// POST /polls/:id/vote
func handleVote(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/polls/")
	idStr = strings.TrimSuffix(idStr, "/vote")
	pollID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	var req VoteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if strings.TrimSpace(req.CPF) == "" || len(req.AnswerIDs) == 0 {
		respondError(w, http.StatusBadRequest, "cpf and answer_ids required")
		return
	}

	prows, err := db.QueryRows(
		`SELECT type, start_date, end_date FROM polls WHERE id = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValString, sqinn.ValString, sqinn.ValString},
	)
	if err != nil || len(prows) == 0 {
		respondError(w, http.StatusNotFound, "poll not found")
		return
	}

	// Hash the CPF before storage
    voterHash := hashCPF(req.CPF)

	row := prows[0]
	pollType := row[0].String
	startDate := row[1].String
	endDate := row[2].String

	if !isPollActive(startDate, endDate) {
		respondError(w, http.StatusGone, "poll is no longer active")
		return
	}

	if pollType == "radio" && len(req.AnswerIDs) > 1 {
		respondError(w, http.StatusBadRequest, "radio poll accepts only one answer")
		return
	}

	for _, ansID := range req.AnswerIDs {
		arows, err := db.QueryRows(
			`SELECT id FROM answers WHERE id = ? AND poll_id = ?`,
			[]sqinn.Value{sqinn.Int64Value(ansID), sqinn.Int64Value(pollID)},
			[]byte{sqinn.ValInt64},
		)
		if err != nil || len(arows) == 0 {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("answer %d not found", ansID))
			return
		}
	}

// Check for existing vote using the hash
	vrows, err := db.QueryRows(
		`SELECT id FROM votes WHERE poll_id = ? AND voter_hash = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID), sqinn.StringValue(voterHash)},
		[]byte{sqinn.ValInt64},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}
	if len(vrows) > 0 {
		respondError(w, http.StatusConflict, "cpf already voted")
		return
	}

	answerIDsJSON, _ := json.Marshal(req.AnswerIDs)
	now := time.Now().UTC().Format(time.RFC3339)

// Store the anonymized hash
	db.MustExecParams(
		`INSERT INTO votes (poll_id, voter_hash, answer_ids, voted_at) VALUES (?, ?, ?, ?)`,
		1, 4,
		[]sqinn.Value{
			sqinn.Int64Value(pollID),
			sqinn.StringValue(voterHash),
			sqinn.StringValue(string(answerIDsJSON)),
			sqinn.StringValue(now),
		},
	)
    
    logAction("VOTE_SUBMITTED", fmt.Sprintf("PollID: %d", pollID))

	respondJSON(w, http.StatusCreated, map[string]bool{"voted": true})
}

// GET /polls/:id/results
func handleResults(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/polls/")
	idStr = strings.TrimSuffix(idStr, "/results")
	pollID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	arows, err := db.QueryRows(
		`SELECT id, text FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValInt64, sqinn.ValString},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}

	answerMap := make(map[int64]ResultAnswer)
	for _, arow := range arows {
		id := arow[0].Int64
		text := arow[1].String
		answerMap[id] = ResultAnswer{ID: id, Text: text, Votes: 0}
	}

	vrows, err := db.QueryRows(
		`SELECT answer_ids FROM votes WHERE poll_id = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValString},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
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

	var results []ResultAnswer
	for _, ans := range answerMap {
		results = append(results, ans)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"poll_id": pollID,
		"answers": results,
	})
}

// ============================================================================
// HTMX UI
// ============================================================================

var uiTemplates = template.Must(template.New("ui").Parse(`
{{define "page"}}
<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>Vote API - POC</title>
<script src="https://unpkg.com/htmx.org@1.9.12"></script>
<style>
  body { font-family: sans-serif; max-width: 600px; margin: 40px auto; padding: 0 20px; }
  fieldset { margin-bottom: 20px; }
  label { display: block; margin-top: 8px; }
  input { display: block; margin-top: 4px; width: 100%; box-sizing: border-box; }
  button { margin-top: 12px; cursor: pointer; }
  .error { color: #c0392b; }
  .ok { color: #27ae60; }
  table { border-collapse: collapse; width: 100%; margin-top: 10px; }
  td, th { border: 1px solid #ccc; padding: 6px 10px; text-align: left; }
  a { cursor: pointer; }
</style>
</head>
<body>
<h1>Vote API - POC</h1>
<div id="app">{{template "auth" .}}</div>
</body>
</html>
{{end}}

{{define "auth"}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<fieldset>
  <legend>1. Request passcode</legend>
  <form hx-post="/ui/request-passcode" hx-target="#app" hx-swap="innerHTML">
    <label>CPF <input name="cpf" required></label>
    <label>Name <input name="name" required></label>
    <label>Phone <input name="phone" required></label>
    <button type="submit">Request passcode</button>
  </form>
</fieldset>
<fieldset>
  <legend>2. Verify passcode</legend>
  <form hx-post="/ui/verify" hx-target="#app" hx-swap="innerHTML">
    <label>CPF <input name="cpf" required></label>
    <label>Passcode <input name="passcode" required></label>
    <button type="submit">Verify</button>
  </form>
</fieldset>
{{end}}

{{define "passcode_sent"}}
<p class="ok">Passcode sent (check the server console - this is a PoC stand-in for WhatsApp).</p>
{{template "auth" .}}
{{end}}

{{define "polls"}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<p>Verified as CPF {{.CPF}}.</p>
<h2>Active polls</h2>
{{if not .Polls}}<p>No active polls right now.</p>{{end}}
<ul>
{{range .Polls}}
  <li><a hx-get="/ui/polls/{{.ID}}?cpf={{$.CPF}}" hx-target="#app" hx-swap="innerHTML">{{.Title}}</a></li>
{{end}}
</ul>
{{end}}

{{define "poll_detail"}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<h2>{{.Poll.Title}}</h2>
<form hx-post="/ui/polls/{{.Poll.ID}}/vote" hx-target="#app" hx-swap="innerHTML">
  <input type="hidden" name="cpf" value="{{.CPF}}">
  {{$type := .Poll.Type}}
  {{range .Poll.Answers}}
  <label>
    <input type="{{if eq $type "radio"}}radio{{else}}checkbox{{end}}" name="answer_ids" value="{{.ID}}" style="display:inline-block;width:auto;">
    {{.Text}}
  </label>
  {{end}}
  <button type="submit">Vote</button>
</form>
<p>
  <a hx-get="/ui/polls/{{.Poll.ID}}/results?cpf={{.CPF}}" hx-target="#app" hx-swap="innerHTML">View results</a>
  &nbsp;|&nbsp;
  <a hx-get="/ui/polls?cpf={{.CPF}}" hx-target="#app" hx-swap="innerHTML">Back to polls</a>
</p>
{{end}}

{{define "vote_result"}}
{{if .Error}}
<p class="error">{{.Error}}</p>
{{else}}
<p class="ok">Vote recorded.</p>
{{end}}
<p><a hx-get="/ui/polls?cpf={{.CPF}}" hx-target="#app" hx-swap="innerHTML">Back to polls</a></p>
{{end}}

{{define "results"}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<h2>Results: {{.Poll.Title}}</h2>
<table>
<tr><th>Answer</th><th>Votes</th></tr>
{{range .Results}}<tr><td>{{.Text}}</td><td>{{.Votes}}</td></tr>{{end}}
</table>
<p><a hx-get="/ui/polls/{{.Poll.ID}}?cpf={{.CPF}}" hx-target="#app" hx-swap="innerHTML">Back to poll</a></p>
{{end}}
`))

type uiPageData struct {
	Error   string
	CPF     string
	Polls   []Poll
	Poll    Poll
	Results []ResultAnswer
}

// UI Handlers
func handleUIIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	uiTemplates.ExecuteTemplate(w, "page", uiPageData{})
}

func handleUIRequestPasscode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()
	cpf := strings.TrimSpace(r.FormValue("cpf"))
	name := strings.TrimSpace(r.FormValue("name"))
	phone := strings.TrimSpace(r.FormValue("phone"))

	if cpf == "" || name == "" || phone == "" {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "cpf, name, phone required"})
		return
	}

	passcode := generatePasscode()
	db.MustExecParams(
		`INSERT INTO voters (cpf, name, phone, passcode, verified_at) VALUES (?, ?, ?, ?, NULL)
		 ON CONFLICT(cpf) DO UPDATE SET passcode=excluded.passcode`,
		1, 4,
		[]sqinn.Value{
			sqinn.StringValue(cpf),
			sqinn.StringValue(name),
			sqinn.StringValue(phone),
			sqinn.StringValue(passcode),
		},
	)

	fmt.Printf("[PoC] CPF %s passcode: %s (for phone %s)\n", cpf, passcode, phone)
	uiTemplates.ExecuteTemplate(w, "passcode_sent", uiPageData{})
}

func handleUIVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()
	cpf := strings.TrimSpace(r.FormValue("cpf"))
	passcode := strings.TrimSpace(r.FormValue("passcode"))

	if cpf == "" || passcode == "" {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "cpf and passcode required"})
		return
	}

	rows, err := db.QueryRows(`SELECT passcode FROM voters WHERE cpf = ?`,
		[]sqinn.Value{sqinn.StringValue(cpf)},
		[]byte{sqinn.ValString},
	)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "db error"})
		return
	}
	if len(rows) == 0 {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "cpf not found"})
		return
	}

	if rows[0][0].String != passcode {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "wrong passcode"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	db.MustExecParams(`UPDATE voters SET verified_at = ? WHERE cpf = ?`,
		1, 2,
		[]sqinn.Value{sqinn.StringValue(now), sqinn.StringValue(cpf)},
	)

	renderUIPolls(w, cpf, "")
}

func renderUIPolls(w http.ResponseWriter, cpf, errMsg string) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := db.QueryRows(
		`SELECT id, title, type, start_date, end_date, created_at FROM polls 
		 WHERE start_date <= ? AND end_date >= ? ORDER BY created_at DESC`,
		[]sqinn.Value{sqinn.StringValue(now), sqinn.StringValue(now)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString},
	)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
		return
	}

	var polls []Poll
	for _, row := range rows {
		var p Poll
		p.ID = row[0].Int64
		p.Title = row[1].String
		p.Type = row[2].String
		p.StartDate = row[3].String
		p.EndDate = row[4].String
		p.CreatedAt = row[5].String
		polls = append(polls, p)
	}

	uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Polls: polls, Error: errMsg})
}

func handleUIPolls(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	renderUIPolls(w, r.URL.Query().Get("cpf"), "")
}

func handleUIPollDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cpf := r.URL.Query().Get("cpf")

	idStr := strings.TrimPrefix(r.URL.Path, "/ui/polls/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "invalid poll id"})
		return
	}

	rows, err := db.QueryRows(
		`SELECT id, title, type, start_date, end_date, created_at FROM polls WHERE id = ?`,
		[]sqinn.Value{sqinn.Int64Value(id)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString},
	)
	if err != nil || len(rows) == 0 {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "poll not found"})
		return
	}

	row := rows[0]
	var p Poll
	p.ID = row[0].Int64
	p.Title = row[1].String
	p.Type = row[2].String
	p.StartDate = row[3].String
	p.EndDate = row[4].String
	p.CreatedAt = row[5].String

	if !isPollActive(p.StartDate, p.EndDate) {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "poll is no longer active"})
		return
	}

	arows, err := db.QueryRows(
		`SELECT id, poll_id, text, display_order FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
		[]sqinn.Value{sqinn.Int64Value(p.ID)},
		[]byte{sqinn.ValInt64, sqinn.ValInt64, sqinn.ValString, sqinn.ValInt32},
	)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
		return
	}

	var answers []Answer
	for _, arow := range arows {
		answers = append(answers, Answer{
			ID:           arow[0].Int64,
			PollID:       arow[1].Int64,
			Text:         arow[2].String,
			DisplayOrder: int(arow[3].Int32),
		})
	}
	p.Answers = answers

	uiTemplates.ExecuteTemplate(w, "poll_detail", uiPageData{CPF: cpf, Poll: p})
}

func handleUIVote(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()
	cpf := strings.TrimSpace(r.FormValue("cpf"))

	idStr := strings.TrimPrefix(r.URL.Path, "/ui/polls/")
	idStr = strings.TrimSuffix(idStr, "/vote")
	pollID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "invalid poll id"})
		return
	}

	answerIDStrs := r.Form["answer_ids"]
	if cpf == "" || len(answerIDStrs) == 0 {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "select an answer"})
		return
	}

	var answerIDs []int64
	for _, s := range answerIDStrs {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "invalid answer id"})
			return
		}
		answerIDs = append(answerIDs, n)
	}

	prows, err := db.QueryRows(`SELECT type, start_date, end_date FROM polls WHERE id = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValString, sqinn.ValString, sqinn.ValString},
	)
	if err != nil || len(prows) == 0 {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "poll not found"})
		return
	}

	row := prows[0]
	pollType := row[0].String
	startDate := row[1].String
	endDate := row[2].String

	if !isPollActive(startDate, endDate) {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "poll is no longer active"})
		return
	}
	if pollType == "radio" && len(answerIDs) > 1 {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "radio poll accepts only one answer"})
		return
	}

	for _, ansID := range answerIDs {
		arows, err := db.QueryRows(`SELECT id FROM answers WHERE id = ? AND poll_id = ?`,
			[]sqinn.Value{sqinn.Int64Value(ansID), sqinn.Int64Value(pollID)},
			[]byte{sqinn.ValInt64},
		)
		if err != nil || len(arows) == 0 {
			uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "answer not found in poll"})
			return
		}
	}

	vrows, err := db.QueryRows(`SELECT id FROM votes WHERE poll_id = ? AND cpf = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID), sqinn.StringValue(cpf)},
		[]byte{sqinn.ValInt64},
	)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "db error"})
		return
	}
	if len(vrows) > 0 {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "cpf already voted on this poll"})
		return
	}

	answerIDsJSON, _ := json.Marshal(answerIDs)
	now := time.Now().UTC().Format(time.RFC3339)
	db.MustExecParams(
		`INSERT INTO votes (poll_id, cpf, answer_ids, voted_at) VALUES (?, ?, ?, ?)`,
		1, 4,
		[]sqinn.Value{
			sqinn.Int64Value(pollID),
			sqinn.StringValue(cpf),
			sqinn.StringValue(string(answerIDsJSON)),
			sqinn.StringValue(now),
		},
	)

	uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf})
}

func handleUIResults(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cpf := r.URL.Query().Get("cpf")

	idStr := strings.TrimPrefix(r.URL.Path, "/ui/polls/")
	idStr = strings.TrimSuffix(idStr, "/results")
	pollID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "invalid poll id"})
		return
	}

	prows, err := db.QueryRows(`SELECT id, title, type, start_date, end_date, created_at FROM polls WHERE id = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString},
	)
	if err != nil || len(prows) == 0 {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "poll not found"})
		return
	}

	row := prows[0]
	var p Poll
	p.ID = row[0].Int64
	p.Title = row[1].String
	p.Type = row[2].String
	p.StartDate = row[3].String
	p.EndDate = row[4].String
	p.CreatedAt = row[5].String

	arows, err := db.QueryRows(`SELECT id, text FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValInt64, sqinn.ValString},
	)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
		return
	}

	answerMap := make(map[int64]*ResultAnswer)
	var order []int64
	for _, arow := range arows {
		id := arow[0].Int64
		text := arow[1].String
		answerMap[id] = &ResultAnswer{ID: id, Text: text, Votes: 0}
		order = append(order, id)
	}

	vrows, err := db.QueryRows(`SELECT answer_ids FROM votes WHERE poll_id = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValString},
	)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
		return
	}

	for _, vrow := range vrows {
		var ids []int64
		json.Unmarshal([]byte(vrow[0].String), &ids)
		for _, id := range ids {
			if a, ok := answerMap[id]; ok {
				a.Votes++
			}
		}
	}

	var results []ResultAnswer
	for _, id := range order {
		if a, ok := answerMap[id]; ok {
			results = append(results, *a)
		}
	}

	uiTemplates.ExecuteTemplate(w, "results", uiPageData{CPF: cpf, Poll: p, Results: results})
}

// ============================================================================
// ROUTER
// ============================================================================

func router(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/auth/request-passcode":
		handleRequestPasscode(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/auth/verify":
		handleVerify(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/polls":
		handleListPolls(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/polls":
		handleCreatePoll(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/polls/") && !strings.Contains(r.URL.Path, "/vote") && !strings.Contains(r.URL.Path, "/results"):
		handleGetPoll(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/polls/") && strings.HasSuffix(r.URL.Path, "/vote"):
		handleVote(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/polls/") && strings.HasSuffix(r.URL.Path, "/results"):
		handleResults(w, r)

	// UI Routes
	case r.Method == http.MethodGet && r.URL.Path == "/":
		handleUIIndex(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/ui/request-passcode":
		handleUIRequestPasscode(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/ui/verify":
		handleUIVerify(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/polls":
		handleUIPolls(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/ui/polls/") && !strings.Contains(r.URL.Path, "/vote") && !strings.Contains(r.URL.Path, "/results"):
		handleUIPollDetail(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/ui/polls/") && strings.HasSuffix(r.URL.Path, "/vote"):
		handleUIVote(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/ui/polls/") && strings.HasSuffix(r.URL.Path, "/results"):
		handleUIResults(w, r)

	default:
		respondError(w, http.StatusNotFound, "endpoint not found")
	}
}

// ============================================================================
// MAIN
// ============================================================================

func main() {
	db = sqinn.MustLaunch(sqinn.Options{Db: "votes.db"})
	defer db.Close()

	if err := initDB(); err != nil {
		log.Fatalf("init db failed: %v", err)
	}

	http.HandleFunc("/", router)

	fmt.Println("Vote API starting on :8080")
	fmt.Println("Endpoints:")
	fmt.Println("  POST   /auth/request-passcode  - Request voting passcode")
	fmt.Println("  POST   /auth/verify             - Verify CPF + passcode")
	fmt.Println("  POST   /polls                   - Create poll (admin)")
	fmt.Println("  GET    /polls                   - List active polls")
	fmt.Println("  GET    /polls/{id}              - Get poll details")
	fmt.Println("  POST   /polls/{id}/vote         - Submit vote")
	fmt.Println("  GET    /polls/{id}/results      - View poll results")
	fmt.Println("\nUI available at http://localhost:8080")

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
