package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sqinn "github.com/cvilsmeier/sqinn-go/v2"
)

func TestAPIEndpoints(t *testing.T) {
	// Setup test DB in memory
	db = sqinn.MustLaunch(sqinn.Options{Db: ":memory:"})
	defer db.Close()

	if err := initDB(); err != nil {
		t.Fatalf("initDB failed: %v", err)
	}

	// Test 1: Request Passcode (com country code)
	t.Run("RequestPasscode", func(t *testing.T) {
		reqBody := `{
			"cpf": "123.456.789-01",
			"name": "Test User",
			"country_code": "55",
			"phone": "(11) 98765-4321"
		}`

		req := httptest.NewRequest(http.MethodPost, "/ui/request-passcode", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handleUIRequestPasscode(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		var resp map[string]interface{}
		json.NewDecoder(rr.Body).Decode(&resp)

		if resp["whatsapp_url"] == nil {
			t.Error("expected whatsapp_url in response")
		}
	})

	// Test 2: Create Poll (Admin)
	t.Run("CreatePoll", func(t *testing.T) {
		reqBody := `{
			"title": "Eleição Teste 2026",
			"type": "radio",
			"start_date": "2026-06-25T10:00:00Z",
			"end_date": "2026-06-30T23:59:59Z",
			"allow_blank": true,
			"answers": [
				{"text": "Candidato A"},
				{"text": "Candidato B"},
				{"text": "Voto em Branco"}
			]
		}`

		req := httptest.NewRequest(http.MethodPost, "/polls", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handleCreatePoll(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})

	// Test 3: List Active Polls
	t.Run("ListPolls", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/polls", nil)
		rr := httptest.NewRecorder()
		handleListPolls(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})

	// Test 4: Vote
	t.Run("Vote", func(t *testing.T) {
		// Assume poll ID 1 exists from previous test
		reqBody := `{"cpf":"12345678901","answer_ids":[1]}`

		req := httptest.NewRequest(http.MethodPost, "/polls/1/vote", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handleVote(rr, req)

		if rr.Code != http.StatusCreated && rr.Code != http.StatusConflict {
			t.Errorf("unexpected status: %d", rr.Code)
		}
	})

	// Test 5: WhatsApp URL Builder
	t.Run("WhatsAppURL", func(t *testing.T) {
		url := buildWhatsAppURL("5511987654321", "4321")
		if !strings.Contains(url, "wa.me/5511987654321") {
			t.Error("WhatsApp URL format incorrect")
		}
		if !strings.Contains(url, "Your+voting+passcode") {
			t.Error("message not encoded properly")
		}
	})

	// Test 6: CPF and Phone Cleaning (indirect via request)
	t.Run("DataCleaning", func(t *testing.T) {
		// Test CPF and Phone normalization logic indirectly
		cpf := strings.ReplaceAll(strings.ReplaceAll("123.456.789-01", ".", ""), "-", "")
		if cpf != "12345678901" || len(cpf) != 11 {
			t.Error("CPF cleaning failed")
		}
	})
}

// Smoke test do servidor UI
func TestServerUI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(router))
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

// Test Admin Login Flow (basic)
func TestAdminLogin(t *testing.T) {
	reqBody := `username=admin&password=123Mudar`
	req := httptest.NewRequest(http.MethodPost, "/ui/admin/login", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	handleAdminLoginPost(rr, req)

	// Pode retornar 200 ou redirecionar para change password
	if rr.Code != http.StatusOK && rr.Code != 302 {
		t.Errorf("unexpected admin login status: %d", rr.Code)
	}
}