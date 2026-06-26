package web

import (
	"fmt"
	"net/http"

	sqinn "github.com/cvilsmeier/sqinn-go/v2"

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

	rows, err := storage.DB.QueryRows(`SELECT id, username, COALESCE(name, ''), COALESCE(phone, ''), is_super, enabled FROM admin WHERE username = ?`,
		[]sqinn.Value{sqinn.StringValue(username)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValInt64, sqinn.ValInt64})

	if err != nil || len(rows) == 0 {
		return nil, fmt.Errorf("admin not found")
	}

	return &models.Admin{
		ID:       rows[0][0].Int64,
		Username: rows[0][1].String,
		Name:     rows[0][2].String,
		Phone:    rows[0][3].String,
		IsSuper:  rows[0][4].Int64 == 1,
		Enabled:  rows[0][5].Int64 == 1,
	}, nil
}
