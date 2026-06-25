package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpsertContact(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/contacts/upsert", strings.NewReader(`{"name":"x"}`))
	rec := httptest.NewRecorder()
	router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "mock-contact-1") {
		t.Fatalf("upsert failed: %s", rec.Body.String())
	}
}
