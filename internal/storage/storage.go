// Package storage owns the database connection, schema creation/seeding and
// small persistence helpers shared by the rest of the application.
package storage

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"time"

	sqinn "github.com/cvilsmeier/sqinn-go/v2"

	"github.com/waldirborbajr/govote/internal/security"
)

// DB is the process-wide SQLite (sqinn) handle. It is set by MustOpen, or
// directly by tests using an in-memory database.
var DB *sqinn.Sqinn

// MustOpen launches sqinn against the given database path and stores the handle
// in DB. It panics on failure.
func MustOpen(path string) *sqinn.Sqinn {
	DB = sqinn.MustLaunch(sqinn.Options{Db: path})
	return DB
}

// BoolToInt converts a bool to the int64 representation stored in SQLite.
func BoolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// LogAction appends an entry to the audit_logs table.
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

// InitDB creates the schema (if needed) and seeds a default admin and a sample
// poll on first run.
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

	// Criar Admin
	rows, _ := DB.QueryRows("SELECT id FROM admin WHERE username = 'admin'", []sqinn.Value{}, []byte{sqinn.ValInt64})
	if len(rows) == 0 {
		// Senha inicial aleatória em vez de um valor fixo conhecido
		// publicamente (o antigo "123Mudar" estava hardcoded no código-fonte
		// deste repositório, então qualquer instância nova ficava vulnerável
		// até alguém logar e trocá-la). needs_change=1 continua forçando a
		// troca no primeiro login, mas agora a senha inicial não é previsível.
		initialPassword, err := generateRandomPassword()
		if err != nil {
			log.Fatalf("falha ao gerar senha inicial do admin: %v", err)
		}
		defaultHash := security.HashPassword(initialPassword)
		now := time.Now().UTC().Format(time.RFC3339)
		DB.MustExecParams(
			`INSERT INTO admin (username, password_hash, is_super, needs_change, created_at) VALUES (?, ?, 1, 1, ?)`,
			1, 3,
			[]sqinn.Value{
				sqinn.StringValue("admin"),
				sqinn.StringValue(defaultHash),
				sqinn.StringValue(now),
			},
		)
		log.Println("✅ Admin padrão criado.")
		log.Printf("🔑 Senha inicial do admin (só exibida agora, troque no primeiro login): %s", initialPassword)
	}

	// Inserir Enquete de Teste
	rows, _ = DB.QueryRows("SELECT count(*) FROM polls WHERE title = ?", []sqinn.Value{sqinn.StringValue("Qual cor prefere?")}, []byte{sqinn.ValInt64})
	if rows[0][0].Int64 == 0 {
		// Insere a enquete
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

		// Recupera o ID da última enquete (usando a variável rows existente)
		rows, _ = DB.QueryRows("SELECT id FROM polls ORDER BY id DESC LIMIT 1", []sqinn.Value{}, []byte{sqinn.ValInt64})
		pollID := rows[0][0].Int64

		// Insere as opções
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

// generateRandomPassword returns a random 16-character hex string suitable as
// a one-time initial password (paired with needs_change=1 to force a reset).
func generateRandomPassword() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
