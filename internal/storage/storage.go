package storage

import (
	"database/sql"
	"log"

	sqinn "github.com/cvilsmeier/sqinn-go/v2"
)

var DB *sqinn.DB

func Init() {
	var err error
	DB, err = sqinn.Open("sqlite", "govote.db")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	// Create tables if not exist (simplified)
	DB.MustExec(`CREATE TABLE IF NOT EXISTS admin (
		id INTEGER PRIMARY KEY,
		username TEXT UNIQUE,
		name TEXT,
		phone TEXT,
		password_hash TEXT,
		passcode TEXT,
		needs_change INTEGER DEFAULT 0,
		is_super INTEGER DEFAULT 0,
		enabled INTEGER DEFAULT 1,
		created_at TEXT
	)`)

	log.Println("Database initialized")
}
