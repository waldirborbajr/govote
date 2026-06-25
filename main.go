package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	sqinn "github.com/cvilsmeier/sqinn-go/v2"
)

// ============================================================================
// TYPES
// ============================================================================

type Poll struct {
	ID         int64    `json:"id"`
	Title      string   `json:"title"`
	Type       string   `json:"type"`
	StartDate  string   `json:"start_date"`
	EndDate    string   `json:"end_date"`
	Answers    []Answer `json:"answers"`
	AllowBlank int64    `json:"allow_blank"`
	CreatedBy  int64    `json:"created_by"`
	CreatedAt  string   `json:"created_at"`
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
	Title      string `json:"title"`
	Type       string `json:"type"`
	StartDate  string `json:"start_date"`
	EndDate    string `json:"end_date"`
	AllowBlank bool   `json:"allow_blank"`
	Answers    []struct {
		Text string `json:"text"`
	} `json:"answers"`
}

type VoteReq struct {
	CPF       string  `json:"cpf"`
	AnswerIDs []int64 `json:"answer_ids"`
}

type Admin struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"` // matches CPF for normal admins
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	IsSuper     bool   `json:"is_super"`
	Enabled     bool   `json:"enabled"`
	Passcode    string `json:"-"`
	NeedsChange bool   `json:"needs_change"`
}

type AdminLoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

var jwtSecret = []byte("super-secret-jwt-key-change-in-production-2026")

// Rate Limiting
type RateLimiter struct {
	visits sync.Map // IP -> []time.Time
}

var rateLimiter = &RateLimiter{}

const (
	maxRequestsPerMinute = 10
	windowDuration       = 60 * time.Second
)

// ============================================================================
// GLOBAL DB & CONFIG
// ============================================================================

var db *sqinn.Sqinn

const hashSalt = "super-secret-salt-value"

// ============================================================================
// SECURITY & HELPERS
// ============================================================================

func hashCPF(cpf string) string {
	h := sha256.New()
	h.Write([]byte(cpf + hashSalt))
	return hex.EncodeToString(h.Sum(nil))
}

func hashPassword(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	salted := append(salt, []byte(password)...)
	hash := sha256.Sum256(salted)
	return hex.EncodeToString(append(salt, hash[:]...))
}

func checkPassword(storedHash, password string) bool {
	raw, err := hex.DecodeString(storedHash)
	if err != nil || len(raw) < 16+sha256.Size {
		return false
	}
	salt := raw[:16]
	expectedHash := raw[16:]

	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(password))
	return hex.EncodeToString(h.Sum(nil)) == hex.EncodeToString(expectedHash)
}

func generateJWT(username string) string {
	expiry := time.Now().Add(24 * time.Hour).Unix()
	return fmt.Sprintf("%s|%d", username, expiry)
}

func validateJWT(token string) (string, bool) {
	parts := strings.Split(token, "|")
	if len(parts) != 2 {
		return "", false
	}
	username := parts[0]
	expiryStr := parts[1]
	expiry, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return "", false
	}
	return username, true
}

// Helper to retrieve current logged in admin database record
func getAuthenticatedAdmin(r *http.Request) (*Admin, error) {
	cookie, err := r.Cookie("admin_token")
	if err != nil {
		return nil, err
	}
	username, valid := validateJWT(cookie.Value)
	if !valid {
		return nil, fmt.Errorf("invalid token")
	}

	rows, err := db.QueryRows(`SELECT id, username, COALESCE(name, ''), COALESCE(phone, ''), is_super, enabled FROM admin WHERE username = ?`,
		[]sqinn.Value{sqinn.StringValue(username)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValInt64, sqinn.ValInt64})

	if err != nil || len(rows) == 0 {
		return nil, fmt.Errorf("admin not found")
	}

	return &Admin{
		ID:       rows[0][0].Int64,
		Username: rows[0][1].String,
		Name:     rows[0][2].String,
		Phone:    rows[0][3].String,
		IsSuper:  rows[0][4].Int64 == 1,
		Enabled:  rows[0][5].Int64 == 1,
	}, nil
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

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
			passcode TEXT,
			verified_at TEXT,
			used_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS polls (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			type TEXT NOT NULL,
			start_date TEXT NOT NULL,
			end_date TEXT NOT NULL,
			allow_blank INTEGER NOT NULL DEFAULT 0,
			created_by INTEGER NOT NULL DEFAULT 1,
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
		`CREATE TABLE IF NOT EXISTS admin (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT,
			name TEXT,
			phone TEXT,
			is_super INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			passcode TEXT,
			needs_change INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
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

// Criar Admin
    rows, _ := db.QueryRows("SELECT id FROM admin WHERE username = 'admin'", []sqinn.Value{}, []byte{sqinn.ValInt64})
    if len(rows) == 0 {
        defaultHash := hashPassword("123Mudar")
        now := time.Now().UTC().Format(time.RFC3339)
        db.MustExecParams(
            `INSERT INTO admin (username, password_hash, created_at) VALUES (?, ?, ?)`,
            1, 3,
            []sqinn.Value{
                sqinn.StringValue("admin"),
                sqinn.StringValue(defaultHash),
                sqinn.StringValue(now),
            },
        )
        log.Println("✅ Admin padrão criado.")
    }

    // Inserir Enquete de Teste
    rows, _ = db.QueryRows("SELECT count(*) FROM polls WHERE title = ?", []sqinn.Value{sqinn.StringValue("Qual cor prefere?")}, []byte{sqinn.ValInt64})
    if rows[0][0].Int64 == 0 {
        // Insere a enquete
        db.MustExecParams(
            `INSERT INTO polls (title, type, start_date, end_date, allow_blank, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
            1, 6,
            []sqinn.Value{
                sqinn.StringValue("Qual cor prefere?"),
                sqinn.StringValue("radio"),
                sqinn.StringValue("2025-01-01T00:00:00Z"),
                sqinn.StringValue("2026-12-31T23:59:59Z"),
                sqinn.Int64Value(0),
                sqinn.StringValue(time.Now().UTC().Format(time.RFC3339)),
            },
        )
        
        // Recupera o ID da última enquete (usando a variável rows existente)
        rows, _ = db.QueryRows("SELECT id FROM polls ORDER BY id DESC LIMIT 1", []sqinn.Value{}, []byte{sqinn.ValInt64})
        pollID := rows[0][0].Int64
        
        // Insere as opções
        cores := []string{"Azul", "Branco", "Vermelho", "Verde", "Preto"}
        for i, cor := range cores {
            db.MustExecParams(
                `INSERT INTO answers (poll_id, text, display_order) VALUES (?, ?, ?)`,
                1, 3,
                []sqinn.Value{
                    sqinn.Int64Value(pollID),
                    sqinn.StringValue(cor),
                    sqinn.Int64Value(int64(i)),
                },
            )
        }
        log.Println("✅ Enquete de teste inserida.")
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
	text := fmt.Sprintf("Your voting system passcode is: %s\n\nDo not share this code with anyone.", passcode)
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

func isRateLimited(ip string) bool {
	now := time.Now()
	var times []time.Time

	if val, ok := rateLimiter.visits.Load(ip); ok {
		times = val.([]time.Time)
	}

	var validTimes []time.Time
	for _, t := range times {
		if now.Sub(t) < windowDuration {
			validTimes = append(validTimes, t)
		}
	}

	if len(validTimes) >= maxRequestsPerMinute {
		rateLimiter.visits.Store(ip, validTimes)
		return true
	}

	validTimes = append(validTimes, now)
	rateLimiter.visits.Store(ip, validTimes)
	return false
}

func getClientIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		ip = r.RemoteAddr
	}
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

func rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)
		if isRateLimited(ip) {
			respondError(w, http.StatusTooManyRequests, "Muitas requisições. Aguarde um momento.")
			return
		}
		next.ServeHTTP(w, r)
	}
}

// ============================================================================
// VOTE & POLL API HANDLERS
// ============================================================================

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
	fmt.Printf("[PoC] CPF %s passcode: %s\n", req.CPF, passcode)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"whatsapp_url": whatsappURL,
		"message":      "Código gerado com sucesso!",
	})
}

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
		`SELECT passcode, used_at FROM voters WHERE cpf = ?`,
		[]sqinn.Value{sqinn.StringValue(req.CPF)},
		[]byte{sqinn.ValString, sqinn.ValString},
	)
	if err != nil || len(rows) == 0 {
		respondError(w, http.StatusUnauthorized, "cpf not found")
		return
	}

	storedPasscode := rows[0][0].String
	usedAt := rows[0][1].String

	if storedPasscode != req.Passcode {
		respondError(w, http.StatusUnauthorized, "wrong passcode")
		return
	}

	if usedAt != "" {
		respondError(w, http.StatusUnauthorized, "este código já foi utilizado. Solicite um novo.")
		return
	}

	db.MustExecParams(
		`UPDATE voters SET passcode = NULL, used_at = ? WHERE cpf = ?`,
		1, 2,
		[]sqinn.Value{
			sqinn.StringValue(time.Now().UTC().Format(time.RFC3339)),
			sqinn.StringValue(req.CPF),
		},
	)

	respondJSON(w, http.StatusOK, map[string]interface{}{"verified": true, "cpf": req.CPF})
}

func handleListPolls(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339)

	rows, err := db.QueryRows(
		`SELECT id, title, type, start_date, end_date, created_by, created_at 
		 FROM polls 
		 WHERE start_date <= ? AND end_date >= ?
		 ORDER BY created_at DESC`,
		[]sqinn.Value{sqinn.StringValue(now), sqinn.StringValue(now)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValInt64, sqinn.ValString},
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
		p.CreatedBy = row[5].Int64
		p.CreatedAt = row[6].String

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

func handleGetPoll(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/polls/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	rows, err := db.QueryRows(
		`SELECT id, title, type, start_date, end_date, created_by, created_at FROM polls WHERE id = ?`,
		[]sqinn.Value{sqinn.Int64Value(id)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValInt64, sqinn.ValString},
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
	p.CreatedBy = row[5].Int64
	p.CreatedAt = row[6].String

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

func handleCreatePoll(w http.ResponseWriter, r *http.Request) {
	admin, err := getAuthenticatedAdmin(r)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Unauthorized admin connection")
		return
	}

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
		`INSERT INTO polls (title, type, start_date, end_date, allow_blank, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		1, 7,
		[]sqinn.Value{
			sqinn.StringValue(req.Title),
			sqinn.StringValue(req.Type),
			sqinn.StringValue(req.StartDate),
			sqinn.StringValue(req.EndDate),
			sqinn.Int64Value(boolToInt(req.AllowBlank)),
			sqinn.Int64Value(admin.ID),
			sqinn.StringValue(now),
		},
	)

	rows, _ := db.QueryRows("SELECT id FROM polls ORDER BY id DESC LIMIT 1", nil, []byte{sqinn.ValInt64})
	if len(rows) == 0 {
		respondError(w, http.StatusInternalServerError, "error retrieving poll id")
		return
	}
	lastInsertID := rows[0][0].Int64

	for i, answer := range req.Answers {
		text := strings.TrimSpace(answer.Text)
		if text == "" {
			continue
		}

		db.MustExecParams(
			`INSERT INTO answers (poll_id, text, display_order) VALUES (?, ?, ?)`,
			1, 3,
			[]sqinn.Value{
				sqinn.Int64Value(lastInsertID),
				sqinn.StringValue(text),
				sqinn.Int64Value(int64(i)),
			},
		)
	}

	handleListPolls(w, r)
}

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

	if voteErr := castVote(pollID, req.CPF, req.AnswerIDs); voteErr != nil {
		respondError(w, voteErr.status, voteErr.message)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]bool{"voted": true})
}

type voteError struct {
	status  int
	message string
}

func (e *voteError) Error() string { return e.message }

func castVote(pollID int64, cpf string, answerIDs []int64) *voteError {
	if strings.TrimSpace(cpf) == "" || len(answerIDs) == 0 {
		return &voteError{http.StatusBadRequest, "cpf and answer_ids required"}
	}

	prows, err := db.QueryRows(
		`SELECT type, start_date, end_date FROM polls WHERE id = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValString, sqinn.ValString, sqinn.ValString},
	)
	if err != nil || len(prows) == 0 {
		return &voteError{http.StatusNotFound, "poll not found"}
	}

	voterHash := hashCPF(cpf)

	row := prows[0]
	pollType := row[0].String
	startDate := row[1].String
	endDate := row[2].String

	if !isPollActive(startDate, endDate) {
		return &voteError{http.StatusGone, "poll is no longer active"}
	}

	if pollType == "radio" && len(answerIDs) > 1 {
		return &voteError{http.StatusBadRequest, "radio poll accepts only one answer"}
	}

	for _, ansID := range answerIDs {
		arows, err := db.QueryRows(
			`SELECT id FROM answers WHERE id = ? AND poll_id = ?`,
			[]sqinn.Value{sqinn.Int64Value(ansID), sqinn.Int64Value(pollID)},
			[]byte{sqinn.ValInt64},
		)
		if err != nil || len(arows) == 0 {
			return &voteError{http.StatusBadRequest, fmt.Sprintf("answer %d not found", ansID)}
		}
	}

	vrows, err := db.QueryRows(
		`SELECT id FROM votes WHERE poll_id = ? AND voter_hash = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID), sqinn.StringValue(voterHash)},
		[]byte{sqinn.ValInt64},
	)
	if err != nil {
		return &voteError{http.StatusInternalServerError, "db error"}
	}
	if len(vrows) > 0 {
		return &voteError{http.StatusConflict, "cpf already voted"}
	}

	answerIDsJSON, _ := json.Marshal(answerIDs)
	now := time.Now().UTC().Format(time.RFC3339)

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

	db.MustExecParams(
		`UPDATE voters SET passcode = NULL, used_at = ? WHERE cpf = ?`,
		1, 2,
		[]sqinn.Value{
			sqinn.StringValue(now),
			sqinn.StringValue(cpf),
		},
	)

	logAction("VOTE_SUBMITTED", fmt.Sprintf("PollID: %d", pollID))
	return nil
}

func simulateNotification(pollID int64, results []ResultAnswer) {
	log.Printf("[NOTIFICATION SIMULATION] Poll ID %d ended. Results: %+v", pollID, results)
}

func handleResults(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/polls/")
	idStr = strings.TrimSuffix(idStr, "/results")
	pollID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	prows, err := db.QueryRows(`SELECT end_date FROM polls WHERE id = ?`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValString},
	)
	if err != nil || len(prows) == 0 {
		respondError(w, http.StatusNotFound, "poll not found")
		return
	}
	pollEndDate, _ := time.Parse(time.RFC3339, prows[0][0].String)

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

	if time.Now().After(pollEndDate) {
		simulateNotification(pollID, results)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"poll_id": pollID,
		"answers": results,
	})
}

// ============================================================================
// HTMX UI TEMPLATES
// ============================================================================

var uiTemplates = template.Must(template.New("ui").Parse(`
{{define "page"}}
<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>Vote API - PoC</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <link href="https://cdn.jsdelivr.net/npm/daisyui@4.12.10/dist/full.min.css" rel="stylesheet" type="text/css" />
  <script src="https://unpkg.com/htmx.org@1.9.12"></script>
  
  <script>
    function formatCPF(input) {
      let v = input.value.replace(/\D/g, '');
      v = v.replace(/(\d{3})(\d)/, '$1.$2');
      v = v.replace(/(\d{3})(\d)/, '$1.$2');
      v = v.replace(/(\d{3})(\d{1,2})$/, '$1-$2');
      input.value = v.substring(0, 14);
    }

    function formatPhone(input) {
      let v = input.value.replace(/\D/g, '');
      if (v.length > 11) v = v.substring(0, 11);
      if (v.length <= 10) {
        v = v.replace(/(\d{2})(\d)/, '($1) $2');
        v = v.replace(/(\d{4})(\d)/, '$1-$2');
      } else {
        v = v.replace(/(\d{2})(\d{5})(\d{4})/, '($1) $2-$3');
      }
      input.value = v;
    }
  </script>
</head>
<body class="bg-base-200 min-h-screen p-4 md:p-8">
  <div class="max-w-3xl mx-auto bg-base-100 p-8 rounded-3xl shadow-2xl">
    <h1 class="text-4xl font-bold mb-2 text-center text-primary">🗳️ Vote API</h1>
    <p class="text-center text-base-content/70 mb-10">Sistema de Votação Simples e Seguro</p>
    
    <div id="app">{{template "index" .}}</div>
  </div>
</body>
</html>
{{end}}

{{define "index"}}
<div class="grid grid-cols-1 md:grid-cols-2 gap-6">
  <div class="card bg-base-200 shadow-xl p-8 hover:shadow-2xl transition-all">
    <div class="text-center mb-6">
      <div class="text-5xl mb-4">🗳️</div>
      <h2 class="text-2xl font-bold mb-2">Votar</h2>
      <p class="text-base-content/70">Participe das enquetes ativas</p>
    </div>
    <button hx-get="/ui/voting-flow" hx-target="#app" class="btn btn-primary btn-lg w-full">
      Acessar Votação
    </button>
  </div>

  <div class="card bg-base-200 shadow-xl p-8 hover:shadow-2xl transition-all">
    <div class="text-center mb-6">
      <div class="text-5xl mb-4">⚙️</div>
      <h2 class="text-2xl font-bold mb-2">Administração</h2>
      <p class="text-base-content/70">Gerenciar enquetes e resultados</p>
    </div>
    <button hx-get="/ui/admin" hx-target="#app" class="btn btn-secondary btn-lg w-full">
      Entrar como Administrador
    </button>
  </div>
</div>
{{end}}

{{define "voting_flow"}}
<div class="card bg-base-100 shadow-xl p-8">
  <h2 class="text-2xl font-bold mb-6 text-center">🗳️ Área de Votação</h2>
  {{if .Error}}<div class="alert alert-error mb-6">{{.Error}}</div>{{end}}

  <div class="grid gap-4">
    <button hx-get="/ui/request-passcode-form" hx-target="#app" class="btn btn-primary btn-lg">
      📱 Gerar Código de Acesso
    </button>
    <div class="divider">OU</div>
    <button hx-get="/ui/verify-form" hx-target="#app" class="btn btn-outline btn-lg">
      🔑 Já tenho código (Entrar)
    </button>
  </div>
  <button hx-get="/" hx-target="#app" class="btn btn-ghost mt-8 w-full">← Voltar</button>
</div>
{{end}}

{{define "admin_dashboard"}}
<div class="space-y-6">
  <h2 class="text-3xl font-bold text-center">Painel Administrativo</h2>
  <p class="text-center text-sm font-semibold">Logado como: <span class="text-primary">{{.AdminUser.Username}}</span></p>
  
  <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
    <button hx-get="/ui/polls/create" hx-target="#app" class="btn btn-primary h-24 text-lg">
      ➕ Criar Nova Enquete
    </button>
    
    <button hx-get="/ui/admin/polls" hx-target="#app" class="btn btn-secondary h-24 text-lg">
      📊 Ver Minhas Enquetes
    </button>
    
    <button hx-get="/admin/stats" hx-target="#app" class="btn btn-accent h-24 text-lg">
      📈 Estatísticas Globais
    </button>

    {{if .AdminUser.IsSuper}}
    <button hx-get="/ui/admin/manage-admins" hx-target="#app" class="btn btn-warning h-24 text-lg md:col-span-2">
      👥 Gerenciar Administradores
    </button>
    {{end}}
  </div>

  <button hx-get="/" hx-target="#app" class="btn btn-ghost w-full">← Voltar ao Início</button>
</div>
{{end}}

{{define "manage_admins"}}
<div class="space-y-6">
  <h2 class="text-2xl font-bold text-center text-warning">👥 Gerenciar Administradores</h2>
  {{if .Error}}<div class="alert alert-error mb-4">{{.Error}}</div>{{end}}
  {{if .Message}}<div class="alert alert-success mb-4">{{.Message}}</div>{{end}}

  <form hx-post="/ui/admin/manage-admins" hx-target="#app" class="card bg-base-200 p-6 space-y-4 shadow-md">
    <h3 class="text-lg font-bold">Cadastrar Novo Administrador</h3>
    <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
      <div class="form-control">
        <label class="label"><span class="label-text">Nome</span></label>
        <input name="name" placeholder="Nome Completo" class="input input-bordered" required>
      </div>
      <div class="form-control">
        <label class="label"><span class="label-text">CPF</span></label>
        <input name="cpf" placeholder="000.000.000-00" onkeyup="formatCPF(this)" class="input input-bordered" maxlength="14" required>
      </div>
      <div class="form-control">
        <label class="label"><span class="label-text">Celular</span></label>
        <input name="phone" placeholder="(11) 98765-4321" onkeyup="formatPhone(this)" class="input input-bordered" maxlength="15" required>
      </div>
      <div class="form-control">
        <label class="label"><span class="label-text">Status Inicial</span></label>
        <select name="enabled" class="select select-bordered">
          <option value="true" selected>Ativo (True)</option>
          <option value="false">Inativo (False)</option>
        </select>
      </div>
    </div>
    <button type="submit" class="btn btn-primary w-full mt-2">Salvar Novo Admin</button>
  </form>

  <div class="card bg-base-100 p-6 shadow-md overflow-x-auto">
    <h3 class="text-lg font-bold mb-4">Administradores Existentes</h3>
    <table class="table table-zebra w-full">
      <thead>
        <tr>
          <th>Nome</th>
          <th>CPF / Usuário</th>
          <th>Celular</th>
          <th>Função</th>
          <th>Status</th>
        </tr>
      </thead>
      <tbody>
        {{range .AdminsList}}
        <tr>
          <td>{{.Name}}</td>
          <td>{{.Username}}</td>
          <td>{{.Phone}}</td>
          <td>{{if .IsSuper}}<span class="badge badge-error">Super Admin</span>{{else}}<span class="badge badge-ghost">Normal</span>{{end}}</td>
          <td>{{if .Enabled}}<span class="text-success font-bold">Ativo</span>{{else}}<span class="text-error font-bold">Inativo</span>{{end}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>

  <button hx-get="/ui/admin" hx-target="#app" class="btn btn-ghost w-full">← Voltar ao Painel</button>
</div>
{{end}}

{{define "passcode_sent"}}
<div class="card bg-base-100 shadow-xl p-8 text-center space-y-6">
  <h2 class="text-3xl font-bold text-success">✅ Código Gerado!</h2>
  <p class="text-lg">Envie o código pelo WhatsApp para continuar.</p>
  {{if .WhatsAppURL}}
  <a href="{{.WhatsAppURL}}" target="_blank" class="btn btn-primary btn-lg w-full">📱 Abrir WhatsApp</a>
  {{end}}
  <button hx-get="/ui/voting-flow" hx-target="#app" class="btn btn-outline w-full">Voltar</button>
</div>
{{end}}

{{define "admin_passcode_sent"}}
<div class="card bg-base-100 shadow-xl p-8 text-center space-y-6">
  <h2 class="text-3xl font-bold text-success">✅ Token enviado para o WhatsApp!</h2>
  <p class="text-lg">Use o link abaixo para acionar a mensagem simulada e em seguida insira o código na tela de login.</p>
  {{if .WhatsAppURL}}
  <a href="{{.WhatsAppURL}}" target="_blank" class="btn btn-primary btn-lg w-full">📱 Enviar Código via WhatsApp</a>
  {{end}}
  <button hx-get="/ui/admin" hx-target="#app" class="btn btn-outline w-full">Ir para tela de Login</button>
</div>
{{end}}

{{define "auth"}}
{{if .Error}}<div class="alert alert-error mb-6 shadow-sm">{{.Error}}</div>{{end}}
<div class="grid gap-8">
  <form hx-post="/ui/request-passcode" hx-target="#app" hx-swap="innerHTML" class="card bg-base-200 p-6 space-y-4">
    <h2 class="text-xl font-bold">1. Solicitar Acesso</h2>
    <div class="form-control">
      <label class="label"><span class="label-text">CPF</span></label>
      <input name="cpf" id="cpf" placeholder="000.000.000-00" class="input input-bordered w-full" maxlength="14" onkeyup="formatCPF(this)" required>
    </div>
    <div class="form-control">
      <label class="label"><span class="label-text">Nome Completo</span></label>
      <input name="name" placeholder="Nome" class="input input-bordered w-full" required>
    </div>
    <div class="form-control">
      <label class="label"><span class="label-text">Celular (com DDD)</span></label>
      <div class="join w-full">
        <select name="country_code" class="select select-bordered join-item w-28">
          <option value="55" selected>Brasil (+55)</option>
          <option value="1">EUA/Canadá (+1)</option>
        </select>
        <input name="phone" id="phone" placeholder="(11) 98765-4321" class="input input-bordered join-item flex-1" onkeyup="formatPhone(this)" maxlength="15" required>
      </div>
    </div>
    <button class="btn btn-primary w-full">Gerar Código de Acesso</button>
  </form>
  
  <form hx-post="/ui/verify" hx-target="#app" hx-swap="innerHTML" class="card bg-base-200 p-6 space-y-4">
    <h2 class="text-xl font-bold">2. Verificar</h2>
    <input name="cpf" placeholder="CPF" class="input input-bordered w-full" required>
    <input name="passcode" placeholder="Passcode" class="input input-bordered w-full" required>
    <button class="btn btn-secondary w-full">Entrar</button>
  </form>
</div>
{{end}}

{{define "poll_detail"}}
<form hx-post="/ui/polls/{{.Poll.ID}}/vote" hx-target="#app" class="space-y-6">
  <input type="hidden" name="cpf" value="{{.CPF}}">
  <h2 class="text-2xl font-bold">{{.Poll.Title}}</h2>
  <div class="form-control gap-3">
    {{$type := .Poll.Type}}
    {{range .Poll.Answers}}
    <label class="label cursor-pointer justify-start gap-4 border p-4 rounded-lg hover:bg-base-200">
      <input type="{{if eq $type "radio"}}radio{{else}}checkbox{{end}}" name="answer_ids" value="{{.ID}}" class="{{if eq $type "radio"}}radio{{else}}checkbox{{end}}">
      <span class="label-text text-lg">{{.Text}}</span>
    </label>
    {{end}}
  </div>
  <button class="btn btn-success w-full">Confirmar Voto</button>
</form>
{{end}}

{{define "vote_result"}}
<div class="card bg-base-100 shadow-xl p-8 text-center space-y-6">
  {{if .Error}}
  <h2 class="text-2xl font-bold text-error">⚠️ Não foi possível registrar seu voto</h2>
  <div class="alert alert-error">{{.Error}}</div>
  {{else}}
  <h2 class="text-3xl font-bold text-success">✅ Voto registrado!</h2>
  <p class="text-lg">Obrigado por participar.</p>
  {{end}}
  <button hx-get="/ui/polls?cpf={{.CPF}}" hx-target="#app" class="btn btn-outline w-full">Voltar às enquetes</button>
</div>
{{end}}

{{define "results"}}
<div class="space-y-6">
  <h2 class="text-2xl font-bold">Resultados: {{.Poll.Title}}</h2>
  <div class="overflow-x-auto">
    <table class="table table-zebra w-full">
      <thead><tr><th>Opção</th><th>Votos</th></tr></thead>
      <tbody>
        {{range .Results}}<tr><td>{{.Text}}</td><td class="font-bold">{{.Votes}}</td></tr>{{end}}
      </tbody>
    </table>
  </div>
  <button hx-get="/ui/admin/polls" hx-target="#app" class="btn btn-ghost w-full">Voltar</button>
</div>
{{end}}

{{define "create_poll"}}
<div class="card bg-base-100 shadow-xl p-6 max-w-lg mx-auto">
  <h2 class="text-2xl font-bold mb-6 text-primary">Criar Nova Enquete</h2>
  <form hx-post="/ui/polls/create" hx-target="#app" class="space-y-4">
    <div class="form-control">
      <label class="label"><span class="label-text">Título</span></label>
      <input name="title" placeholder="Ex: Votação da CIPA" class="input input-bordered w-full" required>
    </div>
    
    <div class="grid grid-cols-2 gap-4">
      <div class="form-control">
        <label class="label"><span class="label-text">Início</span></label>
        <input name="start_date" type="datetime-local" class="input input-bordered" required>
      </div>
      <div class="form-control">
        <label class="label"><span class="label-text">Fim</span></label>
        <input name="end_date" type="datetime-local" class="input input-bordered" required>
      </div>
    </div>

    <div class="form-control">
      <label class="label"><span class="label-text">Tipo</span></label>
      <select name="type" class="select select-bordered w-full">
        <option value="radio">Seleção Única</option>
        <option value="checkbox">Múltipla Escolha</option>
      </select>
    </div>

    <label class="label cursor-pointer justify-start gap-4">
      <input type="checkbox" name="allow_blank" class="checkbox checkbox-primary" value="true">
      <span class="label-text">Permitir voto em branco</span>
    </label>

    <div class="form-control">
      <label class="label"><span class="label-text">Opções (uma por linha)</span></label>
      <textarea name="answers" class="textarea textarea-bordered h-24" required></textarea>
    </div>
    
    <button type="submit" class="btn btn-primary w-full mt-4">Publicar Enquete</button>
  </form>
  <button hx-get="/ui/admin" hx-target="#app" class="btn btn-ghost w-full mt-2">Cancelar</button>
</div>
{{end}}

{{define "polls"}}
<div class="space-y-4">
  <h2 class="text-2xl font-bold">Enquetes Administradas</h2>
  {{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
  {{if .Message}}<div class="alert alert-success">{{.Message}}</div>{{end}}
  <ul class="space-y-2">
    {{range .Polls}}
    <li class="flex gap-2">
       <button hx-get="/ui/polls/{{.ID}}/results" hx-target="#app" class="btn btn-outline flex-1 text-left justify-between">
         <span>{{.Title}}</span>
         <span class="text-xs font-normal text-gray-400">Ver Resultados</span>
       </button>
    </li>
    {{else}}
    <p class="text-gray-500">Nenhuma enquete encontrada.</p>
    {{end}}
  </ul>
  <button hx-get="/ui/admin" hx-target="#app" class="btn btn-ghost w-full mt-4">← Painel Administrativo</button>
</div>
{{end}}

{{define "verify_form"}}
<div class="card bg-base-100 shadow-xl p-8">
  <h2 class="text-2xl font-bold mb-6">🔑 Verificar Acesso</h2>
  <form hx-post="/ui/verify" hx-target="#app" hx-swap="innerHTML" class="space-y-4">
    <input name="cpf" placeholder="CPF" class="input input-bordered w-full" required>
    <input name="passcode" placeholder="Código de 4 dígitos" class="input input-bordered w-full" required>
    <button class="btn btn-secondary w-full">Entrar</button>
  </form>
  <button hx-get="/ui/voting-flow" hx-target="#app" class="btn btn-ghost w-full mt-4">← Voltar</button>
</div>
{{end}}

{{define "admin_login"}}
<div class="card bg-base-100 shadow-xl p-8 max-w-md mx-auto space-y-6">
  <h2 class="text-2xl font-bold text-center text-secondary">🔐 Login Administrador</h2>
  {{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
  
  <form hx-post="/ui/admin/request-otp" hx-target="#app" class="bg-base-200 p-4 rounded-xl space-y-2">
    <span class="text-sm font-semibold text-gray-500 block">Usuários Normais: Solicite senha dinâmica via WhatsApp</span>
    <input name="username" placeholder="Seu CPF de Admin" class="input input-bordered w-full" required>
    <button class="btn btn-sm btn-outline btn-secondary w-full">Receber Senha via WhatsApp</button>
  </form>

  <div class="divider">ENTRAR</div>

  <form hx-post="/ui/admin/login" hx-target="#app" class="space-y-4">
    <input name="username" placeholder="Usuário (admin ou seu CPF)" class="input input-bordered w-full" required>
    <input name="password" type="password" placeholder="Senha (Fixa p/ SuperAdmin, Dinâmica p/ Normais)" class="input input-bordered w-full" required>
    <button class="btn btn-primary w-full">Entrar</button>
  </form>
  <button hx-get="/" hx-target="#app" class="btn btn-ghost w-full">← Voltar</button>
</div>
{{end}}

{{define "admin_change_password"}}
<div class="card bg-base-100 shadow-xl p-8 max-w-md mx-auto">
  <h2 class="text-2xl font-bold mb-6 text-center text-warning">🔄 Troca de Senha Obrigatória</h2>
  <form hx-post="/ui/admin/change-password" hx-target="#app" class="space-y-4">
    <input name="old_password" type="password" placeholder="Senha atual" class="input input-bordered w-full" required>
    <input name="new_password" type="password" placeholder="Nova senha (mín. 8 caracteres)" class="input input-bordered w-full" required>
    <button class="btn btn-primary w-full">Alterar Senha</button>
  </form>
</div>
{{end}}
`))

type uiPageData struct {
	Error       string
	Message     string
	CPF         string
	Polls       []Poll
	Poll        Poll
	Results     []ResultAnswer
	WhatsAppURL string
	AdminUser   *Admin
	AdminsList  []Admin
}

// ============================================================================
// UI HANDLERS
// ============================================================================

func handleUIIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	uiTemplates.ExecuteTemplate(w, "page", uiPageData{})
}

func handleUIVerifyForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	uiTemplates.ExecuteTemplate(w, "verify_form", uiPageData{})
}

func handleUIVotingFlow(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	uiTemplates.ExecuteTemplate(w, "voting_flow", uiPageData{})
}

func handleUIRequestPasscodeForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	uiTemplates.ExecuteTemplate(w, "auth", uiPageData{})
}

func handleUIAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	admin, err := getAuthenticatedAdmin(r)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "admin_login", uiPageData{})
		return
	}

	uiTemplates.ExecuteTemplate(w, "admin_dashboard", uiPageData{AdminUser: admin})
}

func handleUIAdminPolls(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	admin, err := getAuthenticatedAdmin(r)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "admin_login", uiPageData{Error: "Sessão expirada."})
		return
	}
	renderUIAdminPollsList(w, admin, "")
}

func renderUIAdminPollsList(w http.ResponseWriter, admin *Admin, msg string) {
	var rows [][]sqinn.Value
	var err error

	if admin.IsSuper {
		rows, err = db.QueryRows(
			`SELECT id, title, type, start_date, end_date, created_by, created_at FROM polls ORDER BY created_at DESC`,
			nil, []byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValInt64, sqinn.ValString},
		)
	} else {
		rows, err = db.QueryRows(
			`SELECT id, title, type, start_date, end_date, created_by, created_at FROM polls WHERE created_by = ? ORDER BY created_at DESC`,
			[]sqinn.Value{sqinn.Int64Value(admin.ID)},
			[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValInt64, sqinn.ValString},
		)
	}

	if err != nil {
		uiTemplates.ExecuteTemplate(w, "polls", uiPageData{Error: "Erro ao carregar enquetes do banco."})
		return
	}

	var polls []Poll
	for _, row := range rows {
		polls = append(polls, Poll{
			ID:        row[0].Int64,
			Title:     row[1].String,
			Type:      row[2].String,
			StartDate: row[3].String,
			EndDate:   row[4].String,
			CreatedBy: row[5].Int64,
			CreatedAt: row[6].String,
		})
	}

	uiTemplates.ExecuteTemplate(w, "polls", uiPageData{Polls: polls, AdminUser: admin, Message: msg})
}

func handleUIRequestPasscode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()

	cpfRaw := strings.TrimSpace(r.FormValue("cpf"))
	name := strings.TrimSpace(r.FormValue("name"))
	countryCode := strings.TrimSpace(r.FormValue("country_code"))
	phoneRaw := strings.TrimSpace(r.FormValue("phone"))

	if cpfRaw == "" || name == "" || phoneRaw == "" {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "cpf, nome e telefone são obrigatórios"})
		return
	}

	cpf := strings.ReplaceAll(strings.ReplaceAll(cpfRaw, ".", ""), "-", "")
	phone := countryCode + strings.ReplaceAll(strings.ReplaceAll(phoneRaw, "(", ""), ")", "")
	phone = strings.ReplaceAll(phone, "-", "")
	phone = strings.ReplaceAll(phone, " ", "")

	if len(cpf) != 11 {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "CPF inválido"})
		return
	}

	passcode := generatePasscode()

	db.MustExecParams(
		`INSERT INTO voters (cpf, name, phone, passcode, verified_at) 
		 VALUES (?, ?, ?, ?, NULL)
		 ON CONFLICT(cpf) DO UPDATE SET passcode=excluded.passcode`,
		1, 4,
		[]sqinn.Value{
			sqinn.StringValue(cpf),
			sqinn.StringValue(name),
			sqinn.StringValue(phone),
			sqinn.StringValue(passcode),
		},
	)

	whatsappURL := buildWhatsAppURL(phone, passcode)
	fmt.Printf("[PoC] CPF %s | Phone %s | Passcode %s\n", cpf, phone, passcode)

	uiTemplates.ExecuteTemplate(w, "passcode_sent", uiPageData{WhatsAppURL: whatsappURL})
}

func handleUIVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()
	cpf := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(r.FormValue("cpf")), ".", ""), "-", "")
	passcode := strings.TrimSpace(r.FormValue("passcode"))

	if cpf == "" || passcode == "" {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "cpf e passcode obrigatórios"})
		return
	}

	rows, err := db.QueryRows(`SELECT passcode, used_at FROM voters WHERE cpf = ?`,
		[]sqinn.Value{sqinn.StringValue(cpf)},
		[]byte{sqinn.ValString, sqinn.ValString},
	)
	if err != nil || len(rows) == 0 {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "cpf não encontrado"})
		return
	}

	if rows[0][0].String != passcode {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "código incorreto"})
		return
	}

	if rows[0][1].String != "" {
		uiTemplates.ExecuteTemplate(w, "auth", uiPageData{Error: "Este código já foi utilizado. Solicite um novo."})
		return
	}

	db.MustExecParams(
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
		polls = append(polls, Poll{
			ID:        row[0].Int64,
			Title:     row[1].String,
			Type:      row[2].String,
			StartDate: row[3].String,
			EndDate:   row[4].String,
			CreatedAt: row[5].String,
		})
	}

	uiTemplates.ExecuteTemplate(w, "polls", uiPageData{CPF: cpf, Polls: polls, Error: errMsg})
}

func handleUIPolls(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	renderUIVoterPolls(w, r.URL.Query().Get("cpf"), "")
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
	var answerIDs []int64
	for _, s := range answerIDStrs {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: "invalid answer id"})
			return
		}
		answerIDs = append(answerIDs, n)
	}

	if voteErr := castVote(pollID, cpf, answerIDs); voteErr != nil {
		uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf, Error: voteErr.message})
		return
	}

	uiTemplates.ExecuteTemplate(w, "vote_result", uiPageData{CPF: cpf})
}

func handleAdminStats(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryRows("SELECT count(DISTINCT voter_hash) FROM votes", nil, []byte{sqinn.ValInt64})
	if err != nil || len(rows) == 0 {
		respondError(w, http.StatusInternalServerError, "db error")
		return
	}
	totalVotes := rows[0][0].Int64

	const totalEligible = 1000.0
	turnout := (float64(totalVotes) / totalEligible) * 100

	trows, _ := db.QueryRows(
		`SELECT strftime('%Y-%m-%dT%H:00:00', voted_at) as hour, count(*) 
         FROM votes GROUP BY hour ORDER BY hour ASC`,
		nil, []byte{sqinn.ValString, sqinn.ValInt64},
	)

	var timeline []map[string]interface{}
	for _, row := range trows {
		timeline = append(timeline, map[string]interface{}{
			"hour":  row[0].String,
			"count": row[1].Int64,
		})
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"total_votes": totalVotes,
		"turnout_pct": turnout,
		"timeline":    timeline,
	})
}

func handleUICreatePollForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	admin, err := getAuthenticatedAdmin(r)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "admin_login", uiPageData{Error: "Faça login para continuar"})
		return
	}
	uiTemplates.ExecuteTemplate(w, "create_poll", uiPageData{AdminUser: admin})
}

func handleUICreatePoll(w http.ResponseWriter, r *http.Request) {
	admin, err := getAuthenticatedAdmin(r)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Sessão expirada ou não autenticada.")
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

	db.MustExecParams(
		`INSERT INTO polls (title, type, start_date, end_date, allow_blank, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		1, 7,
		[]sqinn.Value{
			sqinn.StringValue(title),
			sqinn.StringValue(pType),
			sqinn.StringValue(startDate),
			sqinn.StringValue(endDate),
			sqinn.Int64Value(boolToInt(allowBlank)),
			sqinn.Int64Value(admin.ID),
			sqinn.StringValue(now),
		},
	)

	rows, _ := db.QueryRows("SELECT id FROM polls ORDER BY id DESC LIMIT 1", nil, []byte{sqinn.ValInt64})
	if len(rows) == 0 {
		respondError(w, http.StatusInternalServerError, "error retrieving poll id")
		return
	}
	lastInsertID := rows[0][0].Int64

	for i, text := range answersRaw {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		db.MustExecParams(
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

func handleUIResults(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	admin, err := getAuthenticatedAdmin(r)
	if err != nil {
		uiTemplates.ExecuteTemplate(w, "admin_login", uiPageData{Error: "Acesso restrito."})
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/ui/polls/")
	idStr = strings.TrimSuffix(idStr, "/results")
	pollID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		renderUIAdminPollsList(w, admin, "ID Inválido")
		return
	}

	prows, err := db.QueryRows(`SELECT id, title, type, start_date, end_date, created_by FROM polls WHERE id = ?`,
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
	var p Poll
	p.ID = row[0].Int64
	p.Title = row[1].String
	p.Type = row[2].String
	p.StartDate = row[3].String
	p.EndDate = row[4].String
	p.CreatedBy = row[5].Int64

	arows, err := db.QueryRows(`SELECT id, text FROM answers WHERE poll_id = ? ORDER BY display_order ASC`,
		[]sqinn.Value{sqinn.Int64Value(pollID)},
		[]byte{sqinn.ValInt64, sqinn.ValString},
	)
	if err != nil {
		renderUIAdminPollsList(w, admin, "Erro na leitura de respostas")
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

	uiTemplates.ExecuteTemplate(w, "results", uiPageData{AdminUser: admin, Poll: p, Results: results})
}

// ============================================================================
// ADMIN WORKFLOWS (SUPER ADMIN MANAGEMENT & OPT AUTH)
// ============================================================================

func handleUIRequestAdminOTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()
	usernameRaw := strings.TrimSpace(r.FormValue("username"))
	username := strings.ReplaceAll(strings.ReplaceAll(usernameRaw, ".", ""), "-", "")

	if username == "admin" {
		uiTemplates.ExecuteTemplate(w, "admin_login", uiPageData{Error: "O administrador master utiliza senha fixa."})
		return
	}

	rows, err := db.QueryRows(`SELECT id, phone, enabled FROM admin WHERE username = ?`,
		[]sqinn.Value{sqinn.StringValue(username)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValInt64},
	)
	if err != nil || len(rows) == 0 {
		uiTemplates.ExecuteTemplate(w, "admin_login", uiPageData{Error: "Administrador não localizado ou desativado."})
		return
	}

	if rows[0][2].Int64 == 0 {
		uiTemplates.ExecuteTemplate(w, "admin_login", uiPageData{Error: "Conta administrativa desativada."})
		return
	}

	phone := rows[0][1].String
	passcode := generatePasscode()

	db.MustExecParams(`UPDATE admin SET passcode = ? WHERE id = ?`, 1, 2,
		[]sqinn.Value{sqinn.StringValue(passcode), sqinn.Int64Value(rows[0][0].Int64)})

	whatsappURL := buildWhatsAppURL(phone, passcode)
	fmt.Printf("[PoC WhatsApp Admin Admin OTP Token] User: %s | Passcode: %s\n", username, passcode)

	uiTemplates.ExecuteTemplate(w, "admin_passcode_sent", uiPageData{WhatsAppURL: whatsappURL})
}

func handleAdminLoginPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()
	usernameRaw := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	username := usernameRaw
	if usernameRaw != "admin" {
		username = strings.ReplaceAll(strings.ReplaceAll(usernameRaw, ".", ""), "-", "")
	}

	rows, _ := db.QueryRows(`SELECT id, password_hash, needs_change, is_super, enabled, passcode FROM admin WHERE username = ?`,
		[]sqinn.Value{sqinn.StringValue(username)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValInt64, sqinn.ValInt64, sqinn.ValInt64, sqinn.ValString})

	if len(rows) == 0 {
		uiTemplates.ExecuteTemplate(w, "admin_login", uiPageData{Error: "Credenciais inválidas"})
		return
	}

	if rows[0][4].Int64 == 0 {
		uiTemplates.ExecuteTemplate(w, "admin_login", uiPageData{Error: "Acesso administrativo revogado (Disabled)."})
		return
	}

	if username == "admin" {
		if !checkPassword(rows[0][1].String, password) {
			uiTemplates.ExecuteTemplate(w, "admin_login", uiPageData{Error: "Senha master incorreta."})
			return
		}
		needsChange := rows[0][2].Int64 == 1
		token := generateJWT(username)
		http.SetCookie(w, &http.Cookie{
			Name:     "admin_token",
			Value:    token,
			Path:     "/",
			MaxAge:   86400,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})

		if needsChange {
			uiTemplates.ExecuteTemplate(w, "admin_change_password", uiPageData{})
			return
		}
	} else {
		storedOTP := rows[0][5].String
		if storedOTP == "" || storedOTP != password {
			uiTemplates.ExecuteTemplate(w, "admin_login", uiPageData{Error: "Token dinâmico inválido ou expirado."})
			return
		}
		db.MustExecParams(`UPDATE admin SET passcode = NULL WHERE id = ?`, 1, 1, []sqinn.Value{sqinn.Int64Value(rows[0][0].Int64)})

		token := generateJWT(username)
		http.SetCookie(w, &http.Cookie{
			Name:     "admin_token",
			Value:    token,
			Path:     "/",
			MaxAge:   86400,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
	}

	adminObj := &Admin{
		ID:       rows[0][0].Int64,
		Username: username,
		IsSuper:  rows[0][3].Int64 == 1,
		Enabled:  true,
	}
	uiTemplates.ExecuteTemplate(w, "admin_dashboard", uiPageData{AdminUser: adminObj})
}

func handleAdminChangePassword(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	newPass := r.FormValue("new_password")

	db.MustExecParams(`UPDATE admin SET password_hash = ?, needs_change = 0 WHERE username = ?`,
		1, 2,
		[]sqinn.Value{
			sqinn.StringValue(hashPassword(newPass)),
			sqinn.StringValue("admin"),
		})

	adminObj := &Admin{ID: 1, Username: "admin", IsSuper: true, Enabled: true}
	uiTemplates.ExecuteTemplate(w, "admin_dashboard", uiPageData{AdminUser: adminObj})
}

func handleUIManageAdmins(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	admin, err := getAuthenticatedAdmin(r)
	if err != nil || !admin.IsSuper {
		uiTemplates.ExecuteTemplate(w, "admin_login", uiPageData{Error: "Acesso reservado exclusivamente ao super administrador."})
		return
	}
	renderManageAdminsPage(w, admin, "", "")
}

func handleUIManageAdminsPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	admin, err := getAuthenticatedAdmin(r)
	if err != nil || !admin.IsSuper {
		uiTemplates.ExecuteTemplate(w, "admin_login", uiPageData{Error: "Operação não autorizada."})
		return
	}

	r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	cpfRaw := strings.TrimSpace(r.FormValue("cpf"))
	phoneRaw := strings.TrimSpace(r.FormValue("phone"))
	enabledBool := r.FormValue("enabled") == "true"

	cpf := strings.ReplaceAll(strings.ReplaceAll(cpfRaw, ".", ""), "-", "")
	phone := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(phoneRaw, "(", ""), ")", ""), "-", ""), " ", "")

	if cpf == "" || name == "" || phone == "" {
		renderManageAdminsPage(w, admin, "Preencha todos os campos corretamente.", "")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	db.MustExecParams(
		`INSERT INTO admin (username, name, phone, is_super, enabled, created_at) 
		 VALUES (?, ?, ?, 0, ?, ?)
		 ON CONFLICT(username) DO UPDATE SET name=excluded.name, phone=excluded.phone, enabled=excluded.enabled`,
		1, 5,
		[]sqinn.Value{
			sqinn.StringValue(cpf),
			sqinn.StringValue(name),
			sqinn.StringValue(phone),
			sqinn.Int64Value(boolToInt(enabledBool)),
			sqinn.StringValue(now),
		},
	)

	renderManageAdminsPage(w, admin, "", "Administrador salvo com sucesso!")
}

func renderManageAdminsPage(w http.ResponseWriter, currentAdmin *Admin, errMsg, successMsg string) {
	rows, err := db.QueryRows(`SELECT id, username, COALESCE(name, ''), COALESCE(phone, ''), is_super, enabled FROM admin ORDER BY id DESC`, nil,
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValInt64, sqinn.ValInt64})

	var list []Admin
	if err == nil {
		for _, row := range rows {
			list = append(list, Admin{
				ID:       row[0].Int64,
				Username: row[1].String,
				Name:     row[2].String,
				Phone:    row[3].String,
				IsSuper:  row[4].Int64 == 1,
				Enabled:  row[5].Int64 == 1,
			})
		}
	}

	uiTemplates.ExecuteTemplate(w, "manage_admins", uiPageData{
		AdminUser:  currentAdmin,
		AdminsList: list,
		Error:      errMsg,
		Message:    successMsg,
	})
}

// ============================================================================
// ROUTER
// ============================================================================

func router(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/auth/request-passcode":
		rateLimitMiddleware(handleRequestPasscode)(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/auth/verify":
		rateLimitMiddleware(handleVerify)(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/polls":
		handleListPolls(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/polls":
		handleCreatePoll(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/polls/") && !strings.Contains(r.URL.Path, "/vote") && !strings.Contains(r.URL.Path, "/results"):
		handleGetPoll(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/polls/") && strings.HasSuffix(r.URL.Path, "/vote"):
		rateLimitMiddleware(handleVote)(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/polls/") && strings.HasSuffix(r.URL.Path, "/results"):
		handleResults(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/admin/stats":
		handleAdminStats(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/verify-form":
		handleUIVerifyForm(w, r)
	// UI Routes
	case r.Method == http.MethodGet && r.URL.Path == "/":
		handleUIIndex(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/voting-flow":
		handleUIVotingFlow(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/request-passcode-form":
		handleUIRequestPasscodeForm(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/ui/request-passcode":
		rateLimitMiddleware(handleUIRequestPasscode)(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/ui/verify":
		rateLimitMiddleware(handleUIVerify)(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/ui/admin/request-otp":
		rateLimitMiddleware(handleUIRequestAdminOTP)(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/ui/admin/login":
		rateLimitMiddleware(handleAdminLoginPost)(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/ui/admin/change-password":
		handleAdminChangePassword(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/admin":
		handleUIAdmin(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/admin/polls":
		handleUIAdminPolls(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/polls":
		handleUIPolls(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/ui/polls/") && !strings.Contains(r.URL.Path, "/vote") && !strings.Contains(r.URL.Path, "/results"):
		handleUIPollDetail(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/ui/polls/") && strings.HasSuffix(r.URL.Path, "/vote"):
		handleUIVote(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/ui/polls/") && strings.HasSuffix(r.URL.Path, "/results"):
		handleUIResults(w, r)
	case r.URL.Path == "/ui/polls/create" && r.Method == http.MethodGet:
		handleUICreatePollForm(w, r)
	case r.URL.Path == "/ui/polls/create" && r.Method == http.MethodPost:
		handleUICreatePoll(w, r)
	case r.URL.Path == "/ui/admin/manage-admins" && r.Method == http.MethodGet:
		handleUIManageAdmins(w, r)
	case r.URL.Path == "/ui/admin/manage-admins" && r.Method == http.MethodPost:
		handleUIManageAdminsPost(w, r)
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

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      http.HandlerFunc(router),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		fmt.Println("🚀 Vote API iniciada em http://localhost:8080")
		fmt.Println("   Pressione Ctrl+C para encerrar gracefulmente.")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Server error: %v", err)
		}
	}()

	<-stop
	fmt.Println("\n\n🛑 Sinal de shutdown recebido. Iniciando encerramento graceful...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("⚠️  Erro durante shutdown: %v", err)
	} else {
		fmt.Println("✅ Servidor encerrado com sucesso (todas as sessões ativas foram finalizadas)")
	}

	fmt.Println("💾 Banco de dados fechado.")
}