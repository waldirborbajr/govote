// admin/handlers.go
// Package admin implements the administrator authentication workflows (OTP
// login, password change) and super-admin management of other admins.
package admin

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

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
	var (
		id       int64
		username string
		enabled  int64
	)
	err := storage.DB.QueryRow(
		`SELECT id, username, enabled FROM admin WHERE phone = ?`,
		phone,
	).Scan(&id, &username, &enabled)
	if err != nil {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Administrador não localizado com este telefone."})
		return
	}

	if enabled == 0 {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Conta administrativa desativada."})
		return
	}

	tempPass := security.GenerateTemporaryPassword()

	// Store hashed temp password and mark for change
	if _, err := storage.DB.Exec(
		`UPDATE admin SET passcode = ?, needs_change = 1 WHERE id = ?`,
		security.HashPasscode(tempPass),
		id,
	); err != nil {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Erro ao gerar senha temporária."})
		return
	}

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

	var (
		id           int64
		passwordHash sql.NullString
		needsChange  int64
		isSuper      int64
		enabled      int64
		storedOTP    sql.NullString
	)

	err := storage.DB.QueryRow(
		`SELECT id, password_hash, needs_change, is_super, enabled, passcode FROM admin WHERE username = ?`,
		username,
	).Scan(&id, &passwordHash, &needsChange, &isSuper, &enabled, &storedOTP)

	if err != nil {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Credenciais inválidas"})
		return
	}

	if enabled == 0 {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Acesso administrativo revogado."})
		return
	}

	if !storedOTP.Valid || storedOTP.String == "" || !security.CheckPasscode(storedOTP.String, password) {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Código inválido ou expirado."})
		return
	}

	// Limpa o passcode
	storage.DB.Exec(`UPDATE admin SET passcode = NULL WHERE id = ?`, id)

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
		ID:          id,
		Username:    username,
		IsSuper:     isSuper == 1,
		Enabled:     true,
		NeedsChange: needsChange == 1,
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

	storage.DB.Exec(
		`UPDATE admin SET password_hash = ?, needs_change = 0 WHERE username = ?`,
		security.HashPassword(newPass),
		username,
	)

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

	if _, err := storage.DB.Exec(
		`INSERT INTO admin (username, name, phone, is_super, enabled, created_at)
		 VALUES (?, ?, ?, 0, ?, ?)
		 ON CONFLICT(username) DO UPDATE SET name=excluded.name, phone=excluded.phone, enabled=excluded.enabled`,
		cpf,
		name,
		phone,
		storage.BoolToInt(enabledBool),
		now,
	); err != nil {
		renderManageAdminsPage(w, admin, "Erro ao salvar administrador.", "")
		return
	}

	renderManageAdminsPage(w, admin, "", "Administrador salvo com sucesso!")
}

func renderManageAdminsPage(w http.ResponseWriter, currentAdmin *models.Admin, errMsg, successMsg string) {
	rows, err := storage.DB.Query(
		`SELECT id, username, COALESCE(name, ''), COALESCE(phone, ''), is_super, enabled FROM admin ORDER BY id DESC`,
	)

	var list []models.Admin
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var a models.Admin
			var isSuper, enabled int64
			if err := rows.Scan(&a.ID, &a.Username, &a.Name, &a.Phone, &isSuper, &enabled); err != nil {
				continue
			}
			a.IsSuper = isSuper == 1
			a.Enabled = enabled == 1
			list = append(list, a)
		}
	}

	web.Templates.ExecuteTemplate(w, "manage_admins", web.PageData{
		AdminUser:  currentAdmin,
		AdminsList: list,
		Error:      errMsg,
		Message:    successMsg,
	})
}
