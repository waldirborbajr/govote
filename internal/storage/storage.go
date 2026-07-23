package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"time"

	sqinn "github.com/cvilsmeier/sqinn-go/v2"

	"github.com/waldirborbajr/govote/internal/notify"
	"github.com/waldirborbajr/govote/internal/security"
)

var DB *sqinn.Sqinn

func MustOpen(path string) *sqinn.Sqinn {
	DB = sqinn.MustLaunch(sqinn.Options{Db: path})
	return DB
}

func BoolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func LogAction(action, details string) {
	now := time.Now().UTC().Format(time.RFC3339)
	DB.MustExecParams(
		`INSERT INTO audit_logs (action, details, created_at) VALUES (?, ?, ?)`,
		1, 3,
		[]sqinn.Value{
			sqinn.StringValue(action),
			sqinn.StringValue(details),
			sqinn.StringValue(now),
		},
	)
}

func InitDB() error {
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
		DB.MustExecSql(schema)
	}

	// Criar Admin master (sem senha fixa - usa OTP via WhatsApp)
	rows, _ := DB.QueryRows("SELECT id FROM admin WHERE username = 'admin'", []sqinn.Value{}, []byte{sqinn.ValInt64})
	if len(rows) == 0 {
		adminPhone := os.Getenv("GOVOTE_ADMIN_PHONE")
		if adminPhone == "" {
			adminPhone = "+5511999999999"
		}
		now := time.Now().UTC().Format(time.RFC3339)
		DB.MustExecParams(
			`INSERT INTO admin (username, name, phone, is_super, enabled, needs_change, created_at) VALUES (?, ?, ?, 1, 1, 0, ?)`,
			1, 4,
			[]sqinn.Value{
				sqinn.StringValue("admin"),
				sqinn.StringValue("Super Admin"),
				sqinn.StringValue(adminPhone),
				sqinn.StringValue(now),
			},
		)
		log.Printf("✅ Admin master criado. Telefone: %s (use 'Solicitar Senha' na tela de login)", adminPhone)
	}

	// Inserir Enquete de Teste
	rows, _ = DB.QueryRows("SELECT count(*) FROM polls WHERE title = ?", []sqinn.Value{sqinn.StringValue("Qual cor prefere?")}, []byte{sqinn.ValInt64})
	if rows[0][0].Int64 == 0 {
		DB.MustExecParams(
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

		rows, _ = DB.QueryRows("SELECT id FROM polls ORDER BY id DESC LIMIT 1", []sqinn.Value{}, []byte{sqinn.ValInt64})
		pollID := rows[0][0].Int64

		cores := []string{"Azul", "Branco", "Vermelho", "Verde", "Preto"}
		for i, cor := range cores {
			DB.MustExecParams(
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

func generateRandomPassword() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
