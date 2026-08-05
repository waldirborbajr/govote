// storage.go
//
// This package uses only database/sql with the pure-Go modernc.org/sqlite
// driver (no CGO, no external sqinn process). Every query in the codebase
// must go through storage.DB using the standard database/sql API
// (DB.Query / DB.QueryRow / DB.Exec), never the sqinn-go API.
package storage

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

// DB is the shared database handle. It is nil until MustOpen has been called.
var DB *sql.DB

// MustOpen opens (or creates) the SQLite database at path using the pure-Go
// modernc.org/sqlite driver, verifies the connection with Ping, stores the
// handle in DB and returns it. It exits the process via log.Fatalf if the
// database cannot be opened or reached.
func MustOpen(path string) *sql.DB {
	var err error

	DB, err = sql.Open("sqlite", path)
	if err != nil {
		log.Fatalf("failed opening database: %v", err)
	}

	if err := DB.Ping(); err != nil {
		log.Fatalf("failed connecting database: %v", err)
	}

	log.Printf("SQLite database opened at %s", path)

	return DB
}

// InitDB creates the schema (tables) if they don't exist yet. MustOpen must
// be called first so DB is set.
func InitDB() error {
	if DB == nil {
		return fmt.Errorf("storage: InitDB called before MustOpen")
	}
	return createTables()
}

// BoolToInt converts a bool to the 0/1 representation used for SQLite
// INTEGER columns that store booleans (allow_blank, is_super, enabled, ...).
func BoolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// LogAction writes a lightweight audit-trail entry to the standard logger.
// This is a PoC-level audit log (stdout/stderr), not a persisted table; swap
// this out for a real audit_log table + INSERT if/when durability across
// restarts is required.
func LogAction(action, detail string) {
	log.Printf("[AUDIT] %s | action=%s | %s", time.Now().UTC().Format(time.RFC3339), action, detail)
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
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	cpf         TEXT UNIQUE,
	name        TEXT,
	phone       TEXT,
	passcode    TEXT,
	verified_at TEXT,
	used_at     TEXT
);

CREATE TABLE IF NOT EXISTS votes (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	poll_id    INTEGER NOT NULL,
	voter_hash TEXT    NOT NULL,
	answer_ids TEXT,
	voted_at   TEXT,
	UNIQUE(poll_id, voter_hash)
);
`

	if _, err := DB.Exec(schema); err != nil {
		return fmt.Errorf("database migration error: %w", err)
	}

	log.Println("SQLite schema ready")
	return nil
}
