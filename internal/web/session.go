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
// cookie, returning an error when the token is missing, invalid or unknown.
func GetAuthenticatedAdmin(r *http.Request) (*models.Admin, error) {
	cookie, err := r.Cookie("admin_token")
	if err != nil {
		return nil, err
	}

	username, valid := security.ValidateJWT(cookie.Value)
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
	)

	err = storage.DB.QueryRow(
		`
		SELECT 
			id,
			username,
			COALESCE(name, ''),
			COALESCE(phone, ''),
			is_super,
			enabled
		FROM admin
		WHERE username = ?
		`,
		username,
	).Scan(
		&id,
		&dbUser,
		&name,
		&phone,
		&isSuper,
		&enabled,
	)

	if err != nil {
		return nil, fmt.Errorf("admin not found")
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
