package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	router().ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	if do(http.MethodGet, "/health", "").Code != http.StatusOK {
		t.Fatal("health failed")
	}
}

func TestTelegramWebhook(t *testing.T) {
	update := `{"message":{"message_id":10,"chat":{"id":555},"from":{"id":777,"username":"user","first_name":"Иван"},"text":"Здравствуйте"}}`
	rec := do(http.MethodPost, "/webhooks/telegram", update)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "accepted") {
		t.Fatalf("telegram failed: %s", rec.Body.String())
	}
	if rec2 := do(http.MethodPost, "/webhooks/telegram", update); !strings.Contains(rec2.Body.String(), "ignored") {
		t.Fatalf("expected duplicate ignored: %s", rec2.Body.String())
	}
}

func TestWhatsAppVerify(t *testing.T) {
	rec := do(http.MethodGet, "/webhooks/whatsapp?hub.verify_token=verify_me&hub.challenge=12345", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "12345" {
		t.Fatalf("verify failed: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSend(t *testing.T) {
	msg := `{"message_id":"m1","channel":"telegram","direction":"outbound","conversation_id":"555","contact":{"external_id":"777"},"content":{"type":"text","text":"Привет"},"timestamp":"2026-01-01T00:00:00Z"}`
	rec := do(http.MethodPost, "/send", msg)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "queued") {
		t.Fatalf("send failed: %s", rec.Body.String())
	}
}
