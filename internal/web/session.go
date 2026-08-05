// session.go
package web

import (
	"fmt"
	"net/http"

	"github.com/waldirborbajr/govote/internal/models"
	"github.com/waldirborbajr/govote/internal/security"
	"github.com/waldirborbajr/govote/internal/storage"
)

// GetAuthenticatedAdmin resolves the admin record for the request's admin_token
// cookie, returning an error when the token is missing, invalid, revoked
// (token_version mismatch) or the admin is disabled/unknown.
func GetAuthenticatedAdmin(r *http.Request) (*models.Admin, error) {
	cookie, err := r.Cookie("admin_token")
	if err != nil {
		return nil, err
	}

	username, tokenVersion, valid := security.ValidateJWT(cookie.Value)
	if !valid {
		return nil, fmt.Errorf("invalid token")
	}

	var (
		id       int64
		name     string
		phone    string
		isSuper  int64
		enabled  int64
		dbUser   string
		dbVersion int64
	)

	err = storage.DB.QueryRow(
		`SELECT
			id,
			username,
			COALESCE(name, ''),
			COALESCE(phone, ''),
			is_super,
			enabled,
			COALESCE(token_version, 0)
		 FROM admin
		 WHERE username = ?`,
		username,
	).Scan(
		&id,
		&dbUser,
		&name,
		&phone,
		&isSuper,
		&enabled,
		&dbVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("admin not found")
	}

	if enabled == 0 {
		return nil, fmt.Errorf("admin disabled")
	}

	// Invalidação: troca de senha / logout / reset de OTP incrementam
	// token_version no banco; tokens antigos falham aqui.
	if tokenVersion != dbVersion {
		return nil, fmt.Errorf("token revoked")
	}

	return &models.Admin{
		ID:       id,
		Username: dbUser,
		Name:     name,
		Phone:    phone,
		IsSuper:  isSuper == 1,
		Enabled:  enabled == 1,
	}, nil
}
