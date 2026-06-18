package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	sqinn "github.com/cvilsmeier/sqinn-go/v2"
)

// ============================================================================
// TYPES
// ============================================================================

type Poll struct {
	ID        int64      `json:"id"`
	Title     string     `json:"title"`
	Type      string     `json:"type"` // "radio" or "checkbox"
	StartDate string     `json:"start_date"`
	EndDate   string     `json:"end_date"`
	Answers   []Answer   `json:"answers"`
	CreatedAt string     `json:"created_at"`
}

type Answer struct {
	ID           int64  `json:"id"`
	PollID       int64  `json:"poll_id"`
	Text         string `json:"text"`
	DisplayOrder int    `json:"display_order"`
}

type Vote struct {
	ID        int64  `json:"id"`
	PollID    int64  `json:"poll_id"`
	CPF       string `json:"cpf"`
	AnswerIDs string `json:"answer_ids"` // JSON array as string
	VotedAt   string `json:"voted_at"`
}

type Voter struct {
	CPF        string `json:"cpf"`
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	Passcode   string `json:"passcode"`
	VerifiedAt *string `json:"verified_at"`
}

type ResultAnswer struct {
	ID    int64  `json:"id"`
	Text  string `json:"text"`
	Votes int    `json:"votes"`
}

// Request/Response types
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
	Type      string `json:"type"` // "radio" or "checkbox"
	StartDate string `json:"start_date"` // RFC3339
	EndDate   string `json:"end_date"`   // RFC3339
	Answers   []struct {
		Text string `json:"text"`
	} `json:"answers"`
}

type VoteReq struct {
	CPF       string  `json:"cpf"`
	AnswerIDs []int64 `json:"answer_ids"`
}

// ============================================================================
// GLOBAL DB
// ============================================================================

var db *sqinn.Database

// ============================================================================
// SCHEMA
// ============================================================================

func initDB() error {
	// Create tables
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
			cpf TEXT NOT NULL,
			answer_ids TEXT NOT NULL,
			voted_at TEXT NOT NULL,
			UNIQUE(poll_id, cpf),
			FOREIGN KEY (poll_id) REFERENCES polls(id),
			FOREIGN KEY (cpf) REFERENCES voters(cpf)
		)`,
	}

	for _, schema := range schemas {
		_, err := db.Exec(schema)
		if err != nil {
			return fmt.Errorf("schema creation failed: %w", err)
		}
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
	return strconv.Itoa(int((b[0]<<8|b[1])%10000))
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

	// Upsert voter
	_, err := db.Exec(
		`INSERT INTO voters (cpf, name, phone, passcode, verified_at) VALUES (?, ?, ?, ?, NULL)
		 ON CONFLICT(cpf) DO UPDATE SET passcode=excluded.passcode`,
		req.CPF, req.Name, req.Phone, passcode,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}

	// PoC: log passcode to stdout instead of WhatsApp
	fmt.Printf("[PoC] CPF %s passcode: %s (for phone %s)\n", req.CPF, passcode, req.Phone)

	respondJSON(w, http.StatusOK, map[string]string{"status": "passcode_sent"})
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

	// Query voter
	rows, err := db.Query(
		`SELECT passcode FROM voters WHERE cpf = ?`,
		req.CPF,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	if !rows.Next() {
		respondError(w, http.StatusUnauthorized, "cpf not found")
		return
	}

	var storedPasscode string
	if err := rows.Scan(&storedPasscode); err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}

	if storedPasscode != req.Passcode {
		respondError(w, http.StatusUnauthorized, "wrong passcode")
		return
	}

	// Set verified_at
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(
		`UPDATE voters SET verified_at = ? WHERE cpf = ?`,
		now, req.CPF,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"verified": true,
		"cpf":      req.CPF,
	})
}

// GET /polls
func handleListPolls(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339)

	rows, err := db.Query(
		`SELECT id, title, type, start_date, end_date, created_at 
		 FROM polls 
		 WHERE start_date <= ? AND end_date >= ?
		 ORDER BY created_at DESC`,
		now, now,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	var polls []Poll
	for rows.Next() {
		var p Poll
		if err := rows.Scan(&p.ID, &p.Title, &p.Type, &p.StartDate, &p.EndDate, &p.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, "db error")
			return
		}

		// Get answers
		arows, err := db.Query(
			`SELECT id, poll_id, text, display_order 
			 FROM answers 
			 WHERE poll_id = ? 
			 ORDER BY display_order ASC`,
			p.ID,
		)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "db error")
			return
		}

		var answers []Answer
		for arows.Next() {
			var a Answer
			if err := arows.Scan(&a.ID, &a.PollID, &a.Text, &a.DisplayOrder); err != nil {
				arows.Close()
				respondError(w, http.StatusInternalServerError, "db error")
				return
			}
			answers = append(answers, a)
		}
		arows.Close()

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

	rows, err := db.Query(
		`SELECT id, title, type, start_date, end_date, created_at 
		 FROM polls 
		 WHERE id = ?`,
		id,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	if !rows.Next() {
		respondError(w, http.StatusNotFound, "poll not found")
		return
	}

	var p Poll
	if err := rows.Scan(&p.ID, &p.Title, &p.Type, &p.StartDate, &p.EndDate, &p.CreatedAt); err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}

	// Check if poll is still active
	if !isPollActive(p.StartDate, p.EndDate) {
		respondError(w, http.StatusGone, "poll is no longer active")
		return
	}

	// Get answers
	arows, err := db.Query(
		`SELECT id, poll_id, text, display_order 
		 FROM answers 
		 WHERE poll_id = ? 
		 ORDER BY display_order ASC`,
		id,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer arows.Close()

	var answers []Answer
	for arows.Next() {
		var a Answer
		if err := arows.Scan(&a.ID, &a.PollID, &a.Text, &a.DisplayOrder); err != nil {
			respondError(w, http.StatusInternalServerError, "db error")
			return
		}
		answers = append(answers, a)
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

	// Insert poll
	res, err := db.Exec(
		`INSERT INTO polls (title, type, start_date, end_date, created_at) 
		 VALUES (?, ?, ?, ?, ?)`,
		req.Title, req.Type, req.StartDate, req.EndDate, now,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}

	pollID := res.LastInsertRowid

	// Insert answers
	for i, ans := range req.Answers {
		_, err := db.Exec(
			`INSERT INTO answers (poll_id, text, display_order) 
			 VALUES (?, ?, ?)`,
			pollID, ans.Text, i,
		)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "db error")
			return
		}
	}

	// Fetch and return created poll
	rows, err := db.Query(
		`SELECT id, title, type, start_date, end_date, created_at 
		 FROM polls 
		 WHERE id = ?`,
		pollID,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	if !rows.Next() {
		respondError(w, http.StatusInternalServerError, "failed to fetch created poll")
		return
	}

	var p Poll
	if err := rows.Scan(&p.ID, &p.Title, &p.Type, &p.StartDate, &p.EndDate, &p.CreatedAt); err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}

	arows, err := db.Query(
		`SELECT id, poll_id, text, display_order 
		 FROM answers 
		 WHERE poll_id = ? 
		 ORDER BY display_order ASC`,
		pollID,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer arows.Close()

	var answers []Answer
	for arows.Next() {
		var a Answer
		if err := arows.Scan(&a.ID, &a.PollID, &a.Text, &a.DisplayOrder); err != nil {
			respondError(w, http.StatusInternalServerError, "db error")
			return
		}
		answers = append(answers, a)
	}

	p.Answers = answers
	respondJSON(w, http.StatusCreated, p)
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

	// Get poll
	prows, err := db.Query(
		`SELECT id, type, start_date, end_date FROM polls WHERE id = ?`,
		pollID,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer prows.Close()

	if !prows.Next() {
		respondError(w, http.StatusNotFound, "poll not found")
		return
	}

	var id int64
	var pollType, startDate, endDate string
	if err := prows.Scan(&id, &pollType, &startDate, &endDate); err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}

	// Check if poll is active
	if !isPollActive(startDate, endDate) {
		respondError(w, http.StatusGone, "poll is no longer active")
		return
	}

	// Check poll type
	if pollType == "radio" && len(req.AnswerIDs) > 1 {
		respondError(w, http.StatusBadRequest, "radio poll accepts only one answer")
		return
	}

	// Validate answer IDs exist in this poll
	for _, ansID := range req.AnswerIDs {
		arows, err := db.Query(
			`SELECT id FROM answers WHERE id = ? AND poll_id = ?`,
			ansID, pollID,
		)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "db error")
			return
		}
		if !arows.Next() {
			arows.Close()
			respondError(w, http.StatusBadRequest, fmt.Sprintf("answer %d not found in poll", ansID))
			return
		}
		arows.Close()
	}

	// Check if CPF already voted on this poll
	vrows, err := db.Query(
		`SELECT id FROM votes WHERE poll_id = ? AND cpf = ?`,
		pollID, req.CPF,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer vrows.Close()

	if vrows.Next() {
		respondError(w, http.StatusConflict, "cpf already voted on this poll")
		return
	}

	// Insert vote
	answerIDsJSON, _ := json.Marshal(req.AnswerIDs)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = db.Exec(
		`INSERT INTO votes (poll_id, cpf, answer_ids, voted_at) 
		 VALUES (?, ?, ?, ?)`,
		pollID, req.CPF, string(answerIDsJSON), now,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}

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

	// Get all answers for this poll
	arows, err := db.Query(
		`SELECT id, text FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
		pollID,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer arows.Close()

	answerMap := make(map[int64]ResultAnswer)
	for arows.Next() {
		var id int64
		var text string
		if err := arows.Scan(&id, &text); err != nil {
			respondError(w, http.StatusInternalServerError, "db error")
			return
		}
		answerMap[id] = ResultAnswer{ID: id, Text: text, Votes: 0}
	}

	// Count votes for each answer
	vrows, err := db.Query(
		`SELECT answer_ids FROM votes WHERE poll_id = ?`,
		pollID,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer vrows.Close()

	for vrows.Next() {
		var answerIDsJSON string
		if err := vrows.Scan(&answerIDsJSON); err != nil {
			respondError(w, http.StatusInternalServerError, "db error")
			return
		}

		var answerIDs []int64
		if err := json.Unmarshal([]byte(answerIDsJSON), &answerIDs); err != nil {
			respondError(w, http.StatusInternalServerError, "db error")
			return
		}

		for _, ansID := range answerIDs {
			if ans, ok := answerMap[ansID]; ok {
				ans.Votes++
				answerMap[ansID] = ans
			}
		}
	}

	// Build result
	var resultAnswers []ResultAnswer
	for _, ans := range answerMap {
		resultAnswers = append(resultAnswers, ans)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"poll_id": pollID,
		"answers": resultAnswers,
	})
}

// ============================================================================
// HTMX UI (POC frontend, served alongside the JSON API)
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

// uiPageData is the single view-model shared by every UI fragment.
type uiPageData struct {
	Error   string
	CPF     string
	Polls   []Poll
	Poll    Poll
	Results []ResultAnswer
}

// GET /
func handleUIIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	uiTemplates.ExecuteTemplate(w, "page", uiPageData{})
}

// POST /ui/request-passcode
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
	_, err := db.Exec(
		`INSERT INTO voters (cpf, name, phone, passcode, verified_at) VALUES (?, ?, ?, ?, NULL)
		 ON CONFLICT(cpf) DO UPDATE SET passcode=excluded.passcode`,
		cpf, name, phone, passcode,
	)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "db error"})
		return
	}

	fmt.Printf("[PoC] CPF %s passcode: %s (for phone %s)\n", cpf, passcode, phone)
	uiTemplates.ExecuteTemplate(w, "passcode_sent", uiPageData{})
}

// POST /ui/verify
func handleUIVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()
	cpf := strings.TrimSpace(r.FormValue("cpf"))
	passcode := strings.TrimSpace(r.FormValue("passcode"))

	if cpf == "" || passcode == "" {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "cpf and passcode required"})
		return
	}

	rows, err := db.Query(`SELECT passcode FROM voters WHERE cpf = ?`, cpf)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "db error"})
		return
	}
	defer rows.Close()

	if !rows.Next() {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "cpf not found"})
		return
	}
	var stored string
	if err := rows.Scan(&stored); err != nil {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "db error"})
		return
	}
	if stored != passcode {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "wrong passcode"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE voters SET verified_at = ? WHERE cpf = ?`, now, cpf); err != nil {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "db error"})
		return
	}

	renderUIPolls(w, cpf, "")
}

// renderUIPolls fetches active polls and writes the "polls" fragment.
func renderUIPolls(w http.ResponseWriter, cpf, errMsg string) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := db.Query(
		`SELECT id, title, type, start_date, end_date, created_at FROM polls 
		 WHERE start_date <= ? AND end_date >= ? ORDER BY created_at DESC`,
		now, now,
	)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
		return
	}
	defer rows.Close()

	var polls []Poll
	for rows.Next() {
		var p Poll
		if err := rows.Scan(&p.ID, &p.Title, &p.Type, &p.StartDate, &p.EndDate, &p.CreatedAt); err != nil {
			uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
			return
		}
		polls = append(polls, p)
	}

	uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Polls: polls, Error: errMsg})
}

// GET /ui/polls
func handleUIPolls(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	renderUIPolls(w, r.URL.Query().Get("cpf"), "")
}

// GET /ui/polls/{id}
func handleUIPollDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cpf := r.URL.Query().Get("cpf")

	idStr := strings.TrimPrefix(r.URL.Path, "/ui/polls/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "invalid poll id"})
		return
	}

	rows, err := db.Query(
		`SELECT id, title, type, start_date, end_date, created_at FROM polls WHERE id = ?`, id,
	)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
		return
	}
	defer rows.Close()

	if !rows.Next() {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "poll not found"})
		return
	}

	var p Poll
	if err := rows.Scan(&p.ID, &p.Title, &p.Type, &p.StartDate, &p.EndDate, &p.CreatedAt); err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
		return
	}

	if !isPollActive(p.StartDate, p.EndDate) {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "poll is no longer active"})
		return
	}

	arows, err := db.Query(
		`SELECT id, poll_id, text, display_order FROM answers WHERE poll_id = ? ORDER BY display_order ASC`, id,
	)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
		return
	}
	defer arows.Close()

	var answers []Answer
	for arows.Next() {
		var a Answer
		if err := arows.Scan(&a.ID, &a.PollID, &a.Text, &a.DisplayOrder); err != nil {
			uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
			return
		}
		answers = append(answers, a)
	}
	p.Answers = answers

	uiTemplates.ExecuteTemplate(w, "poll_detail", uiPageData{CPF: cpf, Poll: p})
}

// POST /ui/polls/{id}/vote
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

	prows, err := db.Query(`SELECT type, start_date, end_date FROM polls WHERE id = ?`, pollID)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "db error"})
		return
	}
	defer prows.Close()
	if !prows.Next() {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "poll not found"})
		return
	}
	var pollType, startDate, endDate string
	if err := prows.Scan(&pollType, &startDate, &endDate); err != nil {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "db error"})
		return
	}

	if !isPollActive(startDate, endDate) {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "poll is no longer active"})
		return
	}
	if pollType == "radio" && len(answerIDs) > 1 {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "radio poll accepts only one answer"})
		return
	}

	for _, ansID := range answerIDs {
		arows, err := db.Query(`SELECT id FROM answers WHERE id = ? AND poll_id = ?`, ansID, pollID)
		if err != nil {
			uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "db error"})
			return
		}
		found := arows.Next()
		arows.Close()
		if !found {
			uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "answer not found in poll"})
			return
		}
	}

	vrows, err := db.Query(`SELECT id FROM votes WHERE poll_id = ? AND cpf = ?`, pollID, cpf)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "db error"})
		return
	}
	alreadyVoted := vrows.Next()
	vrows.Close()
	if alreadyVoted {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "cpf already voted on this poll"})
		return
	}

	answerIDsJSON, _ := json.Marshal(answerIDs)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO votes (poll_id, cpf, answer_ids, voted_at) VALUES (?, ?, ?, ?)`,
		pollID, cpf, string(answerIDsJSON), now,
	); err != nil {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "db error"})
		return
	}

	uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf})
}

// GET /ui/polls/{id}/results
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

	prows, err := db.Query(`SELECT id, title, type, start_date, end_date, created_at FROM polls WHERE id = ?`, pollID)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
		return
	}
	defer prows.Close()
	if !prows.Next() {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "poll not found"})
		return
	}
	var p Poll
	if err := prows.Scan(&p.ID, &p.Title, &p.Type, &p.StartDate, &p.EndDate, &p.CreatedAt); err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
		return
	}

	arows, err := db.Query(`SELECT id, text FROM answers WHERE poll_id = ? ORDER BY display_order ASC`, pollID)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
		return
	}
	defer arows.Close()

	answerMap := make(map[int64]*ResultAnswer)
	var order []int64
	for arows.Next() {
		var id int64
		var text string
		if err := arows.Scan(&id, &text); err != nil {
			uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
			return
		}
		answerMap[id] = &ResultAnswer{ID: id, Text: text}
		order = append(order, id)
	}

	vrows, err := db.Query(`SELECT answer_ids FROM votes WHERE poll_id = ?`, pollID)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
		return
	}
	defer vrows.Close()
	for vrows.Next() {
		var answerIDsJSON string
		if err := vrows.Scan(&answerIDsJSON); err != nil {
			uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Error: "db error"})
			return
		}
		var ids []int64
		if err := json.Unmarshal([]byte(answerIDsJSON), &ids); err != nil {
			continue
		}
		for _, id := range ids {
			if a, ok := answerMap[id]; ok {
				a.Votes++
			}
		}
	}

	var results []ResultAnswer
	for _, id := range order {
		results = append(results, *answerMap[id])
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
	default:
		respondError(w, http.StatusNotFound, "endpoint not found")
	}
}

// ============================================================================
// MAIN
// ============================================================================

func main() {
	var err error
	db, err = sqinn.Open("votes.db")
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	if err := initDB(); err != nil {
		log.Fatalf("failed to init db: %v", err)
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

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
