// Package admin implements the administrator authentication workflows (OTP
// login, password change) and super-admin management of other admins.
package admin

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	sqinn "github.com/cvilsmeier/sqinn-go/v2"

	"github.com/waldirborbajr/govote/internal/models"
	"github.com/waldirborbajr/govote/internal/notify"
	"github.com/waldirborbajr/govote/internal/security"
	"github.com/waldirborbajr/govote/internal/storage"
	"github.com/waldirborbajr/govote/internal/web"
)

// HandleUIRequestAdminOTP issues a one-time passcode (via WhatsApp link) for a
// normal admin login.
func HandleUIRequestAdminOTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()

	usernameRaw := strings.TrimSpace(r.FormValue("username"))
	username := strings.ReplaceAll(strings.ReplaceAll(usernameRaw, ".", ""), "-", "")

	if username == "admin" {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "O administrador master utiliza senha fixa."})
		return
	}

	rows, err := storage.DB.QueryRows(`SELECT id, phone, enabled FROM admin WHERE username = ?`,
		[]sqinn.Value{sqinn.StringValue(username)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValInt64},
	)
	if err != nil || len(rows) == 0 {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Administrador não localizado ou desativado."})
		return
	}

	if rows[0][2].Int64 == 0 {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Conta administrativa desativada."})
		return
	}

	phone := rows[0][1].String
	passcode := security.GeneratePasscode()

	storage.DB.MustExecParams(`UPDATE admin SET passcode = ? WHERE id = ?`, 1, 2,
		[]sqinn.Value{
			sqinn.StringValue(security.HashPasscode(passcode)),
			sqinn.Int64Value(rows[0][0].Int64),
		})

	whatsappURL := notify.BuildWhatsAppURL(phone, passcode)
	fmt.Printf("[PoC WhatsApp Admin OTP] User: %s | Passcode: %s\n", username, passcode)

	web.Templates.ExecuteTemplate(w, "admin_passcode_sent", web.PageData{WhatsAppURL: whatsappURL})
}

// HandleAdminLoginPost authenticates an admin (fixed password for the master,
// dynamic OTP for normal admins) and sets the session cookie.
func HandleAdminLoginPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()
	usernameRaw := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	username := usernameRaw
	if usernameRaw != "admin" {
		username = strings.ReplaceAll(strings.ReplaceAll(usernameRaw, ".", ""), "-", "")
	}

	rows, _ := storage.DB.QueryRows(`SELECT id, password_hash, needs_change, is_super, enabled, passcode FROM admin WHERE username = ?`,
		[]sqinn.Value{sqinn.StringValue(username)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValInt64, sqinn.ValInt64, sqinn.ValInt64, sqinn.ValString})

	if len(rows) == 0 {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Credenciais inválidas"})
		return
	}

	if rows[0][4].Int64 == 0 {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Acesso administrativo revogado (Disabled)."})
		return
	}

	if username == "admin" {
		if !security.CheckPassword(rows[0][1].String, password) {
			web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Senha master incorreta."})
			return
		}
		needsChange := rows[0][2].Int64 == 1
		token := security.GenerateJWT(username)
		http.SetCookie(w, &http.Cookie{
			Name:     "admin_token",
			Value:    token,
			Path:     "/",
			MaxAge:   86400,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})

		if needsChange {
			web.Templates.ExecuteTemplate(w, "admin_change_password", web.PageData{})
			return
		}
	} else {
		storedOTP := rows[0][5].String
		if storedOTP == "" || !security.CheckPasscode(storedOTP, password) {
			web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Token dinâmico inválido ou expirado."})
			return
		}
		storage.DB.MustExecParams(`UPDATE admin SET passcode = NULL WHERE id = ?`, 1, 1, []sqinn.Value{sqinn.Int64Value(rows[0][0].Int64)})

		token := security.GenerateJWT(username)
		http.SetCookie(w, &http.Cookie{
			Name:     "admin_token",
			Value:    token,
			Path:     "/",
			MaxAge:   86400,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
	}

	adminObj := &models.Admin{
		ID:       rows[0][0].Int64,
		Username: username,
		IsSuper:  rows[0][3].Int64 == 1,
		Enabled:  true,
	}
	web.Templates.ExecuteTemplate(w, "admin_dashboard", web.PageData{AdminUser: adminObj})
}

// HandleAdminChangePassword updates the master admin password and clears the
// forced-change flag.
func HandleAdminChangePassword(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	newPass := r.FormValue("new_password")

	storage.DB.MustExecParams(`UPDATE admin SET password_hash = ?, needs_change = 0 WHERE username = ?`,
		1, 2,
		[]sqinn.Value{
			sqinn.StringValue(security.HashPassword(newPass)),
			sqinn.StringValue("admin"),
		})

	adminObj := &models.Admin{ID: 1, Username: "admin", IsSuper: true, Enabled: true}
	web.Templates.ExecuteTemplate(w, "admin_dashboard", web.PageData{AdminUser: adminObj})
}

// HandleUIManageAdmins renders the super-admin management page.
func HandleUIManageAdmins(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	admin, err := web.GetAuthenticatedAdmin(r)
	if err != nil || !admin.IsSuper {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Acesso reservado exclusivamente ao super administrador."})
		return
	}
	renderManageAdminsPage(w, admin, "", "")
}

// HandleUIManageAdminsPost creates or updates an admin from the management form.
func HandleUIManageAdminsPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	admin, err := web.GetAuthenticatedAdmin(r)
	if err != nil || !admin.IsSuper {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Operação não autorizada."})
		return
	}

	r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	cpfRaw := strings.TrimSpace(r.FormValue("cpf"))
	phoneRaw := strings.TrimSpace(r.FormValue("phone"))
	enabledBool := r.FormValue("enabled") == "true"

	cpf := strings.ReplaceAll(strings.ReplaceAll(cpfRaw, ".", ""), "-", "")
	phone := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(phoneRaw, "(", ""), ")", ""), "-", ""), " ", "")

	if cpf == "" || name == "" || phone == "" {
		renderManageAdminsPage(w, admin, "Preencha todos os campos corretamente.", "")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	storage.DB.MustExecParams(
		`INSERT INTO admin (username, name, phone, is_super, enabled, created_at)
		 VALUES (?, ?, ?, 0, ?, ?)
		 ON CONFLICT(username) DO UPDATE SET name=excluded.name, phone=excluded.phone, enabled=excluded.enabled`,
		1, 5,
		[]sqinn.Value{
			sqinn.StringValue(cpf),
			sqinn.StringValue(name),
			sqinn.StringValue(phone),
			sqinn.Int64Value(storage.BoolToInt(enabledBool)),
			sqinn.StringValue(now),
		},
	)

	renderManageAdminsPage(w, admin, "", "Administrador salvo com sucesso!")
}

func renderManageAdminsPage(w http.ResponseWriter, currentAdmin *models.Admin, errMsg, successMsg string) {
	rows, err := storage.DB.QueryRows(`SELECT id, username, COALESCE(name, ''), COALESCE(phone, ''), is_super, enabled FROM admin ORDER BY id DESC`, nil,
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValString, sqinn.ValString, sqinn.ValInt64, sqinn.ValInt64})

	var list []models.Admin
	if err == nil {
		for _, row := range rows {
			list = append(list, models.Admin{
				ID:       row[0].Int64,
				Username: row[1].String,
				Name:     row[2].String,
				Phone:    row[3].String,
				IsSuper:  row[4].Int64 == 1,
				Enabled:  row[5].Int64 == 1,
			})
		}
	}

	web.Templates.ExecuteTemplate(w, "manage_admins", web.PageData{
		AdminUser:  currentAdmin,
		AdminsList: list,
		Error:      errMsg,
		Message:    successMsg,
	})
}
