package storage

import (
	"database/sql"
	"os"
	"strconv"
	"time"
)

// Auth lockout defaults (overridable via env).
const (
	defaultMaxFailures = 5
	defaultLockMinutes = 15
)

func maxFailures() int {
	if v := os.Getenv("GOVOTE_LOCKOUT_MAX_FAILURES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultMaxFailures
}

func lockDuration() time.Duration {
	mins := defaultLockMinutes
	if v := os.Getenv("GOVOTE_LOCKOUT_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			mins = n
		}
	}
	return time.Duration(mins) * time.Minute
}

// IsLocked reports whether key (e.g. "admin:user" or "cpf:123...") is currently locked.
// remaining is approximate time left when locked.
func IsLocked(key string) (locked bool, remaining time.Duration) {
	if DB == nil || key == "" {
		return false, 0
	}
	var lockedUntil sql.NullString
	err := DB.QueryRow(
		`SELECT locked_until FROM auth_lockout WHERE key = ?`,
		key,
	).Scan(&lockedUntil)
	if err != nil || !lockedUntil.Valid || lockedUntil.String == "" {
		return false, 0
	}
	until, err := time.Parse(time.RFC3339, lockedUntil.String)
	if err != nil {
		return false, 0
	}
	now := time.Now().UTC()
	if now.Before(until) {
		return true, until.Sub(now)
	}
	// Lock expired — clear.
	_, _ = DB.Exec(
		`UPDATE auth_lockout SET fail_count = 0, locked_until = NULL, updated_at = ? WHERE key = ?`,
		now.Format(time.RFC3339), key,
	)
	return false, 0
}

// RecordFailure increments the failure counter for key and locks when threshold is reached.
func RecordFailure(key string) {
	if DB == nil || key == "" {
		return
	}
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	var count int
	var lockedUntil sql.NullString
	err := DB.QueryRow(
		`SELECT fail_count, locked_until FROM auth_lockout WHERE key = ?`,
		key,
	).Scan(&count, &lockedUntil)
	if err == sql.ErrNoRows {
		_, _ = DB.Exec(
			`INSERT INTO auth_lockout (key, fail_count, locked_until, updated_at) VALUES (?, 1, NULL, ?)`,
			key, nowStr,
		)
		return
	}
	if err != nil {
		return
	}

	// Still under an active lock: do not grow the counter forever.
	if lockedUntil.Valid && lockedUntil.String != "" {
		if until, e := time.Parse(time.RFC3339, lockedUntil.String); e == nil && now.Before(until) {
			return
		}
	}

	count++
	max := maxFailures()
	var untilVal interface{}
	if count >= max {
		untilVal = now.Add(lockDuration()).Format(time.RFC3339)
		count = max
	} else {
		untilVal = nil
	}

	_, _ = DB.Exec(
		`UPDATE auth_lockout SET fail_count = ?, locked_until = ?, updated_at = ? WHERE key = ?`,
		count, untilVal, nowStr, key,
	)
}

// ClearFailures resets the counter after a successful authentication.
func ClearFailures(key string) {
	if DB == nil || key == "" {
		return
	}
	_, _ = DB.Exec(`DELETE FROM auth_lockout WHERE key = ?`, key)
}

// LockoutKeyAdmin builds the lockout key for an admin username.
func LockoutKeyAdmin(username string) string {
	return "admin:" + username
}

// LockoutKeyCPF builds the lockout key for a voter CPF.
func LockoutKeyCPF(cpf string) string {
	return "cpf:" + cpf
}
