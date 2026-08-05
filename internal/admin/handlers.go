// admin/handlers.go
// Package admin implements the administrator authentication workflows (OTP
// login, password change) and super-admin management of other admins.
package admin

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/waldirborbajr/govote/internal/models"
	"github.com/waldirborbajr/govote/internal/notify"
	"github.com/waldirborbajr/govote/internal/security"
	"github.com/waldirborbajr/govote/internal/storage"
	"github.com/waldirborbajr/govote/internal/web"
)

// setAdminCookie grava o cookie de sessão com flags de segurança.
// Secure=true exige HTTPS (já usado pelo app em :8443).
func setAdminCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_token",
		Value:    token,
		Path:     "/",
		MaxAge:   int(security.AdminTokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// clearAdminCookie remove o cookie de sessão (logout).
func clearAdminCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

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

	// Store hashed temp password and mark for change.
	// Incrementa token_version para invalidar sessões antigas.
	if _, err := storage.DB.Exec(
		`UPDATE admin SET passcode = ?, needs_change = 1, token_version = token_version + 1 WHERE id = ?`,
		security.HashPasscode(tempPass),
		id,
	); err != nil {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Erro ao gerar senha temporária."})
		return
	}

	whatsappURL := notify.BuildWhatsAppURL(phone, tempPass)
	
	// o envio real via WhatsApp substitui este log.
	storage.LogAction("ADMIN_TEMP_PASSWORD_ISSUED", "user="+username)
	
	web.Templates.ExecuteTemplate(w, "admin_passcode_sent", web.PageData{WhatsAppURL: whatsappURL})
}

// HandleUIRequestAdminOTP (legacy for 4-digit, kept for compatibility)
func HandleUIRequestAdminOTP(w http.ResponseWriter, r *http.Request) {
	HandleUIRequestAdminTemporaryPassword(w, r)
}

// HandleAdminLoginPost - enhanced to handle needs_change and token_version.
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
		tokenVersion int64
	)

	err := storage.DB.QueryRow(
		`SELECT id, password_hash, needs_change, is_super, enabled, passcode, COALESCE(token_version, 0)
		 FROM admin WHERE username = ?`,
		username,
	).Scan(&id, &passwordHash, &needsChange, &isSuper, &enabled, &storedOTP, &tokenVersion)

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

	// Limpa o passcode (uso único)
	storage.DB.Exec(`UPDATE admin SET passcode = NULL WHERE id = ?`, id)

	token := security.GenerateJWT(username, tokenVersion)
	setAdminCookie(w, token)

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

// HandleAdminChangePassword updated to support any admin.
// Invalida todas as sessões antigas incrementando token_version e emite
// um novo cookie com a versão atualizada.
func HandleAdminChangePassword(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()
	newPass := r.FormValue("new_password")

	if len(newPass) < 8 {
		web.Templates.ExecuteTemplate(w, "admin_change_password", web.PageData{Error: "Senha deve ter no mínimo 8 caracteres."})
		return
	}

	// Preferir admin autenticado; fallback ao form/username "admin"
	var username string
	var isSuper bool
	if adm, err := web.GetAuthenticatedAdmin(r); err == nil && adm != nil {
		username = adm.Username
		isSuper = adm.IsSuper
	} else {
		username = strings.TrimSpace(r.FormValue("username"))
		if username == "" {
			username = "admin"
		}
		isSuper = true // legado: fluxo de primeiro acesso do super
	}

	res, err := storage.DB.Exec(
		`UPDATE admin
		 SET password_hash = ?, needs_change = 0, token_version = token_version + 1
		 WHERE username = ?`,
		security.HashPassword(newPass),
		username,
	)
	if err != nil {
		web.Templates.ExecuteTemplate(w, "admin_change_password", web.PageData{Error: "Erro ao atualizar senha."})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		web.Templates.ExecuteTemplate(w, "admin_change_password", web.PageData{Error: "Administrador não encontrado."})
		return
	}

	var newVersion int64
	_ = storage.DB.QueryRow(
		`SELECT COALESCE(token_version, 0) FROM admin WHERE username = ?`,
		username,
	).Scan(&newVersion)

	token := security.GenerateJWT(username, newVersion)
	setAdminCookie(w, token)

	adminObj := &models.Admin{Username: username, IsSuper: isSuper, Enabled: true}
	web.Templates.ExecuteTemplate(w, "admin_dashboard", web.PageData{AdminUser: adminObj})
}

// HandleAdminLogout invalida a sessão atual (cookie) e incrementa
// token_version para invalidar outros tokens do mesmo admin.
func HandleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if adm, err := web.GetAuthenticatedAdmin(r); err == nil && adm != nil {
		storage.DB.Exec(
			`UPDATE admin SET token_version = token_version + 1 WHERE username = ?`,
			adm.Username,
		)
	}
	clearAdminCookie(w)
	http.Redirect(w, r, "/ui/admin", http.StatusSeeOther)
}

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
		`INSERT INTO admin (username, name, phone, is_super, enabled, token_version, created_at)
		 VALUES (?, ?, ?, 0, ?, 0, ?)
		 ON CONFLICT(username) DO UPDATE SET
		   name=excluded.name,
		   phone=excluded.phone,
		   enabled=excluded.enabled`,
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
