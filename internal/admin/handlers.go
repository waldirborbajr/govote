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

func HandleUIRequestAdminOTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()

	phoneRaw := strings.TrimSpace(r.FormValue("phone"))
	phone := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(phoneRaw, "(", ""), ")", ""), "-", ""), " ", "")

	if phone == "" {
		web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Telefone é obrigatório para solicitar senha."})
		return
	}

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
	passcode := security.GeneratePasscode()

	storage.DB.MustExecParams(`UPDATE admin SET passcode = ? WHERE id = ?`, 1, 2,
		[]sqinn.Value{
			sqinn.StringValue(security.HashPasscode(passcode)),
			sqinn.Int64Value(rows[0][0].Int64),
		})

	whatsappURL := notify.BuildWhatsAppURL(phone, passcode)
	fmt.Printf("[PoC WhatsApp Admin OTP] User: %s | Phone: %s | Passcode: %s\n", username, phone, passcode)

	web.Templates.ExecuteTemplate(w, "admin_passcode_sent", web.PageData{WhatsAppURL: whatsappURL})
}

// HandleAdminLoginPost (mantido com suporte a OTP para todos)
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
		// Master agora também usa OTP (passcode)
		storedOTP := rows[0][5].String
		if storedOTP == "" || !security.CheckPasscode(storedOTP, password) {
			web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Código inválido ou expirado."})
			return
		}
		storage.DB.MustExecParams(`UPDATE admin SET passcode = NULL WHERE id = ?`, 1, 1, []sqinn.Value{sqinn.Int64Value(rows[0][0].Int64)})
	} else {
		storedOTP := rows[0][5].String
		if storedOTP == "" || !security.CheckPasscode(storedOTP, password) {
			web.Templates.ExecuteTemplate(w, "admin_login", web.PageData{Error: "Token dinâmico inválido ou expirado."})
			return
		}
		storage.DB.MustExecParams(`UPDATE admin SET passcode = NULL WHERE id = ?`, 1, 1, []sqinn.Value{sqinn.Int64Value(rows[0][0].Int64)})
	}

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
		ID:       rows[0][0].Int64,
		Username: username,
		IsSuper:  rows[0][3].Int64 == 1,
		Enabled:  true,
	}
	web.Templates.ExecuteTemplate(w, "admin_dashboard", web.PageData{AdminUser: adminObj})
}

// ... resto do arquivo (HandleAdminChangePassword, ManageAdmins etc.) permanece igual
