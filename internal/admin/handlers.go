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

// HandleUIRequestAdminTemporaryPassword handles the new "Solicitar Senha" feature.
func HandleUIRequestAdminTemporaryPassword(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()

	phoneRaw := strings.TrimSpace(r.FormValue("phone"))
	phone := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(phoneRaw, "(", ""), ")", ""), "-", ""), " ", "")

	if phone == "" {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Telefone é obrigatório."})
		return
	}

	// Busca por telefone
	rows, err := storage.DB.QueryRows(`SELECT id, username, enabled FROM admin WHERE phone = ?`,
		[]sqinn.Value{sqinn.StringValue(phone)},
		[]byte{sqinn.ValInt64, sqinn.ValString, sqinn.ValInt64},
	)
	if err != nil || len(rows) == 0 {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Administrador não localizado com este telefone."})
		return
	}

	if rows[0][2].Int64 == 0 {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Conta administrativa desativada."})
		return
	}

	username := rows[0][1].String
	tempPass := security.GenerateTemporaryPassword()

	// Store hashed temp password and mark for change
	storage.DB.MustExecParams(`UPDATE admin SET passcode = ?, needs_change = 1 WHERE id = ?`, 1, 2,
		[]sqinn.Value{
			sqinn.StringValue(security.HashPasscode(tempPass)),
			sqinn.Int64Value(rows[0][0].Int64),
		})

	whatsappURL := notify.BuildWhatsAppURL(phone, tempPass)
	fmt.Printf("[Admin Temp Password] User: %s | Phone: %s | TempPass: %s\n", username, phone, tempPass)

	web.Templates.ExecuteTemplate(w, "admin_passcode_sent", web.PageData{WhatsAppURL: whatsappURL})
}

// HandleUIRequestAdminOTP (legacy for 4-digit, kept for compatibility)
func HandleUIRequestAdminOTP(w http.ResponseWriter, r *http.Request) {
	// ... (existing code, can call new func or keep as is)
	HandleUIRequestAdminTemporaryPassword(w, r) // reuse for now
}

// HandleAdminLoginPost - enhanced to handle needs_change
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
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Acesso administrativo revogado."})
		return
	}

	storedOTP := rows[0][5].String
	if storedOTP == "" || !security.CheckPasscode(storedOTP, password) {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Código inválido ou expirado."})
		return
	}

	// Limpa o passcode
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

	adminObj := &models.Admin{
		ID:         rows[0][0].Int64,
		Username:   username,
		IsSuper:    rows[0][3].Int64 == 1,
		Enabled:    true,
		NeedsChange: rows[0][2].Int64 == 1,
	}

	if adminObj.NeedsChange {
		web.Templates.ExecuteTemplate(w, "admin_change_password", web.PageData{AdminUser: adminObj})
		return
	}

	web.Templates.ExecuteTemplate(w, "admin_dashboard", web.PageData{AdminUser: adminObj})
}

// HandleAdminChangePassword updated to support any admin
func HandleAdminChangePassword(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	newPass := r.FormValue("new_password")

	if len(newPass) < 8 {
		web.Templates.ExecuteTemplate(w, "admin_change_password", web.PageData{Error: "Senha deve ter no mínimo 8 caracteres."})
		return
	}

	username := r.FormValue("username") // if passed, or from session
	if username == "" {
		username = "admin"
	}

	storage.DB.MustExecParams(`UPDATE admin SET password_hash = ?, needs_change = 0 WHERE username = ?`,
		1, 2,
		[]sqinn.Value{
			sqinn.StringValue(security.HashPassword(newPass)),
			sqinn.StringValue(username),
		})

	adminObj := &models.Admin{Username: username, IsSuper: true, Enabled: true}
	web.Templates.ExecuteTemplate(w, "admin_dashboard", web.PageData{AdminUser: adminObj})
}

// ... rest of the file remains the same (Manage Admins etc.)
// HandleUIManageAdmins and others unchanged
func HandleUIManageAdmins(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	admin, err := web.GetAuthenticatedAdmin(r)
	if err != nil || !admin.IsSuper {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Acesso reservado exclusivamente ao super administrador."})
		return
	}
	renderManageAdminsPage(w, admin, "", "")
}

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
