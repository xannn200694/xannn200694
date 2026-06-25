package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func doJSON(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	router().ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	rec := doJSON(t, http.MethodGet, "/health", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestListPrompts(t *testing.T) {
	rec := doJSON(t, http.MethodGet, "/v1/prompts", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "sales_assistant") {
		t.Fatalf("unexpected: %d %s", rec.Code, rec.Body.String())
	}
}

func TestChatBasic(t *testing.T) {
	rec := doJSON(t, http.MethodPost, "/v1/chat", `{"variables":{"user_message":"Здравствуйте"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "trace_id") {
		t.Fatalf("no trace_id: %s", rec.Body.String())
	}
}

func TestChatEscalation(t *testing.T) {
	rec := doJSON(t, http.MethodPost, "/v1/chat", `{"variables":{"user_message":"хочу менеджера"}}`)
	if !strings.Contains(rec.Body.String(), `"should_escalate":true`) {
		t.Fatalf("expected escalation: %s", rec.Body.String())
	}
}

func TestClassify(t *testing.T) {
	rec := doJSON(t, http.MethodPost, "/v1/classify", `{"text":"сколько стоит?"}`)
	if !strings.Contains(rec.Body.String(), "purchase_intent") {
		t.Fatalf("expected purchase_intent: %s", rec.Body.String())
	}
}
