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
	if !strings.Contains(do(http.MethodGet, "/health", "").Body.String(), `"mock":true`) {
		t.Fatal("health failed")
	}
}

func TestChat(t *testing.T) {
	rec := do(http.MethodPost, "/v1/chat", `{"variables":{"user_message":"hi"}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "text") {
		t.Fatalf("chat failed: %s", rec.Body.String())
	}
}
