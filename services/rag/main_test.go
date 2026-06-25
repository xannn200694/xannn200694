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
	if do(http.MethodGet, "/v1/health", "").Code != http.StatusOK {
		t.Fatal("health failed")
	}
}

func TestIngestAndSearch(t *testing.T) {
	ing := `{"documents":[{"doc_id":"d1","title":"Доставка","text":"Доставка по Бишкеку занимает один день.","source":"site"}]}`
	if rec := do(http.MethodPost, "/v1/ingest", ing); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ingested":1`) {
		t.Fatalf("ingest failed: %s", rec.Body.String())
	}
	rec := do(http.MethodPost, "/v1/search", `{"query":"сколько идёт доставка","top_k":3}`)
	if rec.Code != http.StatusOK || !strings.Contains(strings.ToLower(rec.Body.String()), "доставка") {
		t.Fatalf("search failed: %s", rec.Body.String())
	}
}

func TestDelete(t *testing.T) {
	do(http.MethodPost, "/v1/ingest", `{"documents":[{"doc_id":"d2","title":"t","text":"тестовый документ"}]}`)
	if do(http.MethodDelete, "/v1/documents/d2", "").Code != http.StatusOK {
		t.Fatal("delete failed")
	}
	if do(http.MethodDelete, "/v1/documents/d2", "").Code != http.StatusNotFound {
		t.Fatal("expected 404 on second delete")
	}
}
