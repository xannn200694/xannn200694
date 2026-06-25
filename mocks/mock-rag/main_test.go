package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearch(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/search", strings.NewReader(`{"query":"test"}`))
	rec := httptest.NewRecorder()
	router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "0.99") {
		t.Fatalf("search failed: %s", rec.Body.String())
	}
}
