// storage.go
//
// This package uses only database/sql with the pure-Go modernc.org/sqlite
// driver (no CGO). Every query must go through storage.DB.
package storage

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DB is the shared database handle. It is nil until MustOpen has been called.
var DB *sql.DB

// MustOpen opens (or creates) the SQLite database at path.
func MustOpen(path string) *sql.DB {
	var err error

	DB, err = sql.Open("sqlite", path)
	if err != nil {
		log.Fatalf("failed opening database: %v", err)
	}

	// Reasonable defaults for concurrent request handlers.
	DB.SetMaxOpenConns(1) // SQLite: single writer
	DB.SetMaxIdleConns(1)
	DB.SetConnMaxLifetime(0)

	if err := DB.Ping(); err != nil {
		log.Fatalf("failed connecting database: %v", err)
	}

	log.Printf("SQLite database opened at %s", path)
	return DB
}

// InitDB creates the schema and bootstraps the super-admin when the table is empty.
func InitDB() error {
	if DB == nil {
		return fmt.Errorf("storage: InitDB called before MustOpen")
	}
	if err := createTables(); err != nil {
		return err
	}
	return bootstrapSuperAdmin()
}

// BoolToInt converts a bool to the 0/1 representation used for SQLite INTEGER booleans.
func BoolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// LogAction writes an audit entry to stdout and to the persistent audit_log table.
func LogAction(action, detail string) {
	LogActionIP(action, detail, "")
}

// LogActionIP is LogAction with an optional client IP.
func LogActionIP(action, detail, ip string) {
	now := time.Now().UTC().Format(time.RFC3339)
	log.Printf("[AUDIT] %s | action=%s | ip=%s | %s", now, action, ip, detail)
	if DB == nil {
		return
	}
	if _, err := DB.Exec(
		`INSERT INTO audit_log (at, action, detail, ip) VALUES (?, ?, ?, ?)`,
		now, action, detail, ip,
	); err != nil {
		log.Printf("⚠️  falha ao gravar audit_log: %v", err)
	}
}

func createTables() error {
	schema := `
CREATE TABLE IF NOT EXISTS admin (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	username      TEXT    UNIQUE NOT NULL,
	name          TEXT,
	phone         TEXT,
	password_hash TEXT,
	passcode      TEXT,
	needs_change  INTEGER NOT NULL DEFAULT 0,
	is_super      INTEGER NOT NULL DEFAULT 0,
	enabled       INTEGER NOT NULL DEFAULT 1,
	token_version INTEGER NOT NULL DEFAULT 0,
	created_at    TEXT
);

CREATE TABLE IF NOT EXISTS polls (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	title       TEXT    NOT NULL,
	type        TEXT    NOT NULL,
	start_date  TEXT,
	end_date    TEXT,
	allow_blank INTEGER NOT NULL DEFAULT 0,
	created_by  INTEGER,
	created_at  TEXT
);

CREATE TABLE IF NOT EXISTS answers (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	poll_id       INTEGER NOT NULL,
	text          TEXT    NOT NULL,
	display_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS voters (
	id                   INTEGER PRIMARY KEY AUTOINCREMENT,
	cpf                  TEXT UNIQUE,
	name                 TEXT,
	phone                TEXT,
	passcode             TEXT,
	passcode_expires_at  TEXT,
	verified_at          TEXT,
	used_at              TEXT
);

CREATE TABLE IF NOT EXISTS votes (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	poll_id    INTEGER NOT NULL,
	voter_hash TEXT    NOT NULL,
	answer_ids TEXT,
	voted_at   TEXT,
	UNIQUE(poll_id, voter_hash)
);

CREATE TABLE IF NOT EXISTS audit_log (
	id     INTEGER PRIMARY KEY AUTOINCREMENT,
	at     TEXT NOT NULL,
	action TEXT NOT NULL,
	detail TEXT,
	ip     TEXT
);

CREATE TABLE IF NOT EXISTS auth_lockout (
	key         TEXT PRIMARY KEY,
	fail_count  INTEGER NOT NULL DEFAULT 0,
	locked_until TEXT,
	updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_log_at ON audit_log(at);
`

	if _, err := DB.Exec(schema); err != nil {
		return fmt.Errorf("database migration error: %w", err)
	}

	// Migração incremental: telegram_chat_id foi adicionado após o schema
	// inicial, então bancos existentes precisam de ALTER TABLE. SQLite não
	// suporta "ADD COLUMN IF NOT EXISTS", então tentamos e ignoramos o erro
	// de coluna duplicada (banco já migrado).
	if _, err := DB.Exec(`ALTER TABLE voters ADD COLUMN telegram_chat_id TEXT`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("migration error (telegram_chat_id): %w", err)
		}
	}

	log.Println("SQLite schema ready")
	return nil
}

// bootstrapSuperAdmin creates the initial super-admin when the admin table is empty.
// Credentials:
//
//	GOVOTE_BOOTSTRAP_USERNAME (default: "super")
//	GOVOTE_BOOTSTRAP_PHONE    (required — E.164, e.g. +5511999999999)
//	GOVOTE_BOOTSTRAP_NAME     (optional)
//
// No password is set. Login is only via temporary OTP (needs_change=1).
// If the table already has any admin, this is a no-op.
func bootstrapSuperAdmin() error {
	var n int64
	if err := DB.QueryRow(`SELECT COUNT(*) FROM admin`).Scan(&n); err != nil {
		return fmt.Errorf("bootstrap count: %w", err)
	}
	if n > 0 {
		return nil
	}

	phone := strings.TrimSpace(os.Getenv("GOVOTE_BOOTSTRAP_PHONE"))
	if phone == "" {
		log.Println("⚠️  Nenhum admin no banco e GOVOTE_BOOTSTRAP_PHONE não definida — super-admin NÃO criado. Defina o telefone e reinicie, ou crie o admin manualmente.")
		return nil
	}

	username := strings.TrimSpace(os.Getenv("GOVOTE_BOOTSTRAP_USERNAME"))
	if username == "" {
		username = "super"
	}
	name := strings.TrimSpace(os.Getenv("GOVOTE_BOOTSTRAP_NAME"))
	if name == "" {
		name = "Super Admin"
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(
		`INSERT INTO admin (username, name, phone, password_hash, passcode, needs_change, is_super, enabled, token_version, created_at)
		 VALUES (?, ?, ?, NULL, NULL, 1, 1, 1, 0, ?)`,
		username, name, phone, now,
	)
	if err != nil {
		return fmt.Errorf("bootstrap super-admin: %w", err)
	}

	LogAction("BOOTSTRAP_SUPER_ADMIN", "username="+username+" phone="+phone)
	log.Printf("✅ Super-admin bootstrap criado: username=%s phone=%s (sem senha — use OTP/senha temporária e defina senha no primeiro acesso)", username, phone)
	return nil
}
