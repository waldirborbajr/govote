// telegram.go
// Handler for the Telegram/n8n integration: issues a voter passcode the
// same way HandleRequestPasscode does, but returns the plaintext code
// instead of a WhatsApp link, so a trusted bot (run from n8n) can deliver
// it directly in the chat. Protected by web.RequireServiceAPIKey.
package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/waldirborbajr/govote/internal/models"
	"github.com/waldirborbajr/govote/internal/security"
	"github.com/waldirborbajr/govote/internal/storage"
	"github.com/waldirborbajr/govote/internal/validate"
	"github.com/waldirborbajr/govote/internal/web"
)

// HandleTelegramRequestCode generates and stores a voter passcode for a CPF,
// linking it to a Telegram chat_id, and returns the plaintext passcode so
// the calling bot can deliver it. This endpoint is service-to-service only
// (X-API-Key) — never expose it to end users directly, since it defeats the
// anti-enumeration behavior of the public /auth/request-passcode endpoint.
func HandleTelegramRequestCode(w http.ResponseWriter, r *http.Request) {
	var req models.TelegramRequestCodeReq
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := validate.CPF(req.CPF); err != nil {
		web.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validate.Name(req.Name); err != nil {
		web.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validate.TelegramChatID(req.ChatID); err != nil {
		web.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Telefone é opcional aqui (entrega é via Telegram, não WhatsApp); se
	// informado, ainda precisa ser válido.
	if strings.TrimSpace(req.Phone) != "" {
		if err := validate.Phone(req.Phone); err != nil {
			web.RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	req.CPF = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(req.CPF), ".", ""), "-", "")
	req.Name = strings.TrimSpace(req.Name)
	req.Phone = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(req.Phone), "(", ""), ")", ""), "-", ""), " ", "")
	req.ChatID = strings.TrimSpace(req.ChatID)

	passcode := security.GeneratePasscode()
	expiresAt := time.Now().UTC().Add(security.PasscodeTTL).Format(time.RFC3339)

	_, err := storage.DB.Exec(
		`INSERT INTO voters (cpf, name, phone, passcode, passcode_expires_at, telegram_chat_id, verified_at, used_at)
		 VALUES (?, ?, ?, ?, ?, ?, NULL, NULL)
		 ON CONFLICT(cpf) DO UPDATE SET
		   passcode=excluded.passcode,
		   passcode_expires_at=excluded.passcode_expires_at,
		   name=excluded.name,
		   -- Canal Telegram não exige telefone: só sobrescreve o telefone já
		   -- salvo (possivelmente cadastrado via WhatsApp) se um novo valor
		   -- não vazio for enviado. Mantém os dois canais independentes por CPF.
		   phone=CASE WHEN excluded.phone <> '' THEN excluded.phone ELSE phone END,
		   telegram_chat_id=excluded.telegram_chat_id,
		   verified_at=NULL,
		   used_at=NULL`,
		req.CPF,
		req.Name,
		req.Phone,
		security.HashPasscode(passcode),
		expiresAt,
		req.ChatID,
	)
	if err != nil {
		web.RespondError(w, http.StatusInternalServerError, "falha ao gerar passcode")
		return
	}

	storage.LogAction("TELEGRAM_PASSCODE_ISSUED", "cpf_fp="+security.TokenFingerprint(req.CPF))

	web.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"cpf":        req.CPF,
		"passcode":   passcode,
		"expires_at": expiresAt,
	})
}
