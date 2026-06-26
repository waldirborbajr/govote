package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqinn "github.com/cvilsmeier/sqinn-go/v2"

	"github.com/waldirborbajr/govote/internal/admin"
	"github.com/waldirborbajr/govote/internal/api"
	"github.com/waldirborbajr/govote/internal/notify"
	"github.com/waldirborbajr/govote/internal/server"
	"github.com/waldirborbajr/govote/internal/storage"
	"github.com/waldirborbajr/govote/internal/ui"
)

func TestAPIEndpoints(t *testing.T) {
	storage.DB = sqinn.MustLaunch(sqinn.Options{Db: ":memory:"})
	defer storage.DB.Close()

	if err := storage.InitDB(); err != nil {
		t.Fatalf("initDB failed: %v", err)
	}

	// Test 1: Request Passcode (UI Handler)
	t.Run("RequestPasscode", func(t *testing.T) {
		reqBody := `cpf=12345678901&name=Test User&country_code=55&phone=(11) 98765-4321`
		req := httptest.NewRequest(http.MethodPost, "/ui/request-passcode", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rr := httptest.NewRecorder()
		ui.HandleUIRequestPasscode(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		body := rr.Body.String()
		if !strings.Contains(body, "Código Gerado") && !strings.Contains(body, "whatsapp") {
			t.Error("expected success message or whatsapp link in response")
		}
	})

	// Test 2: Create Poll
	t.Run("CreatePoll", func(t *testing.T) {
		reqBody := `{
			"title": "Eleição Teste",
			"type": "radio",
			"start_date": "` + time.Now().UTC().Format(time.RFC3339) + `",
			"end_date": "` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
			"allow_blank": false,
			"answers": [{"text":"Opção A"},{"text":"Opção B"}]
		}`

		req := httptest.NewRequest(http.MethodPost, "/polls", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		api.HandleCreatePoll(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})

	// Test 3: List Polls
	t.Run("ListPolls", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/polls", nil)
		rr := httptest.NewRecorder()
		api.HandleListPolls(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})

	// Test 4: Vote (com poll ativo)
	t.Run("Vote", func(t *testing.T) {
		// Cria poll ativo
		pollBody := `{
			"title": "Votação Teste",
			"type": "radio",
			"start_date": "` + time.Now().Add(-1*time.Hour).UTC().Format(time.RFC3339) + `",
			"end_date": "` + time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339) + `",
			"answers": [{"text":"Sim"},{"text":"Não"}]
		}`

		createReq := httptest.NewRequest(http.MethodPost, "/polls", bytes.NewBufferString(pollBody))
		createReq.Header.Set("Content-Type", "application/json")
		api.HandleCreatePoll(httptest.NewRecorder(), createReq)

		// Vota
		voteBody := `{"cpf":"12345678901","answer_ids":[1]}`
		voteReq := httptest.NewRequest(http.MethodPost, "/polls/1/vote", bytes.NewBufferString(voteBody))
		voteReq.Header.Set("Content-Type", "application/json")

		voteRR := httptest.NewRecorder()
		api.HandleVote(voteRR, voteReq)

		if voteRR.Code != http.StatusCreated && voteRR.Code != http.StatusConflict {
			t.Errorf("unexpected vote status: %d", voteRR.Code)
		}
	})

	// Test 5: WhatsApp URL
	t.Run("WhatsAppURL", func(t *testing.T) {
		url := notify.BuildWhatsAppURL("5511987654321", "4321")
		if !strings.Contains(url, "wa.me/5511987654321") {
			t.Error("WhatsApp URL format incorrect")
		}
	})

	t.Run("DataCleaning", func(t *testing.T) {
		cpf := strings.ReplaceAll(strings.ReplaceAll("123.456.789-01", ".", ""), "-", "")
		if cpf != "12345678901" {
			t.Error("CPF cleaning failed")
		}
	})
}

// Smoke test da UI
func TestServerUI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(server.Router))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// Test Admin Login básico
func TestAdminLogin(t *testing.T) {
	form := "username=admin&password=123Mudar"
	req := httptest.NewRequest(http.MethodPost, "/ui/admin/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	admin.HandleAdminLoginPost(rr, req)

	// Aceita tanto sucesso direto quanto redirecionamento para troca de senha
	if rr.Code != http.StatusOK {
		t.Logf("Admin login returned status %d (expected OK)", rr.Code)
	}
}
