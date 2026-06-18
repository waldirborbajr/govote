package main
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAPIEndpoints(t *testing.T) {
	// Setup test DB
	db = sqinn.MustLaunch(sqinn.Options{Db: ":memory:"}) // Use in-memory for tests if supported, otherwise "test_votes.db"
	defer db.Close()

	if err := initDB(); err != nil {
		t.Fatalf("initDB failed: %v", err)
	}

	// Test 1: Request Passcode
	t.Run("RequestPasscode", func(t *testing.T) {
		reqBody := `{"cpf":"12345678901","name":"Test User","phone":"5511999999999"}`
		req := httptest.NewRequest(http.MethodPost, "/auth/request-passcode", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handleRequestPasscode(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		var resp map[string]interface{}
		json.NewDecoder(rr.Body).Decode(&resp)

		if resp["status"] != "passcode_generated" {
			t.Error("expected status passcode_generated")
		}
		if _, ok := resp["whatsapp_url"]; !ok {
			t.Error("expected whatsapp_url in response")
		}
	})

	// Test 2: Create Poll
	t.Run("CreatePoll", func(t *testing.T) {
		reqBody := `{
			"title": "Test Election",
			"type": "radio",
			"start_date": "2026-06-01T10:00:00Z",
			"end_date": "2026-06-30T23:59:59Z",
			"answers": [{"text":"Option A"},{"text":"Option B"}]
		}`

		req := httptest.NewRequest(http.MethodPost, "/polls", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handleCreatePoll(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})

	// Test 3: List Polls
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
		// First create a poll (reuse logic)
		// For simplicity we assume a poll with ID 1 exists from previous test
		reqBody := `{"cpf":"12345678901","answer_ids":[1]}`
		req := httptest.NewRequest(http.MethodPost, "/polls/1/vote", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handleVote(rr, req)

		// May return 409 if already voted, but basic test
		if rr.Code != http.StatusCreated && rr.Code != http.StatusConflict {
			t.Errorf("unexpected status: %d", rr.Code)
		}
	})

	t.Run("WhatsAppURL", func(t *testing.T) {
		url := buildWhatsAppURL("5511999999999", "1234")
		if !strings.Contains(url, "wa.me/5511999999999") {
			t.Error("WhatsApp URL format incorrect")
		}
		if !strings.Contains(url, "Your+voting+passcode") {
			t.Error("message not encoded properly")
		}
	})
}

// Integration test with full server
func TestServer(t *testing.T) {
	// This is a basic smoke test
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