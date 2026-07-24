
// storage.go
package storage

import (
	"database/sql"
	"log"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func Init() {
	var err error

	DB, err = sql.Open("sqlite", "govote.db")
	if err != nil {
		log.Fatalf("failed opening database: %v", err)
	}

	if err := DB.Ping(); err != nil {
		log.Fatalf("failed connecting database: %v", err)
	}

	createTables()

	log.Println("SQLite database initialized")
}

func createTables() {

	schema := `

CREATE TABLE IF NOT EXISTS admin (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT UNIQUE,
	name TEXT,
	phone TEXT,
	password_hash TEXT,
	passcode TEXT,
	needs_change INTEGER DEFAULT 0,
	is_super INTEGER DEFAULT 0,
	enabled INTEGER DEFAULT 1,
	created_at TEXT
);

CREATE TABLE IF NOT EXISTS polls (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL,
	type TEXT NOT NULL,
	start_date TEXT,
	end_date TEXT,
	allow_blank INTEGER DEFAULT 0,
	created_by INTEGER,
	created_at TEXT
);

CREATE TABLE IF NOT EXISTS answers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	poll_id INTEGER NOT NULL,
	text TEXT NOT NULL,
	display_order INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS voters (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	cpf TEXT UNIQUE,
	name TEXT,
	phone TEXT,
	passcode TEXT,
	verified_at TEXT,
	used_at TEXT
);

CREATE TABLE IF NOT EXISTS votes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	poll_id INTEGER,
	voter_hash TEXT,
	answer_ids TEXT,
	voted_at TEXT
);

`

	_, err := DB.Exec(schema)
	if err != nil {
		log.Fatalf("database migration error: %v", err)
	}
}
