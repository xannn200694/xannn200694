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

func TestMain(m *testing.M) {
	adapter = getAdapter()
	m.Run()
}

func TestHealth(t *testing.T) {
	rec := do(http.MethodGet, "/health", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "provider") {
		t.Fatalf("health failed: %s", rec.Body.String())
	}
}

func TestContactUpsertAndLookup(t *testing.T) {
	payload := `{"name":"Иван","phone":"+996700000000"}`
	rec := do(http.MethodPost, "/v1/contacts/upsert", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert failed: %d", rec.Code)
	}
	id := extractID(rec.Body.String())
	if id == "" {
		t.Fatal("no id returned")
	}
	rec2 := do(http.MethodPost, "/v1/contacts/upsert", payload)
	if extractID(rec2.Body.String()) != id {
		t.Fatal("idempotent upsert by phone failed")
	}
	found := do(http.MethodGet, "/v1/contacts/by-phone/+996700000000", "")
	if found.Code != http.StatusOK || !strings.Contains(found.Body.String(), id) {
		t.Fatalf("lookup failed: %s", found.Body.String())
	}
}

func TestLeadAndTask(t *testing.T) {
	if do(http.MethodPost, "/v1/leads/upsert", `{"contact":{"name":"Лид","phone":"+996700000001"},"title":"Заявка"}`).Code != http.StatusOK {
		t.Fatal("lead failed")
	}
	if do(http.MethodPost, "/v1/tasks", `{"text":"Перезвонить"}`).Code != http.StatusOK {
		t.Fatal("task failed")
	}
}

func TestCRMWebhook(t *testing.T) {
	if do(http.MethodPost, "/webhooks/crm", `{"event":"deal.update"}`).Code != http.StatusOK {
		t.Fatal("webhook failed")
	}
}

func extractID(body string) string {
	const key = `"id":"`
	i := strings.Index(body, key)
	if i < 0 {
		return ""
	}
	rest := body[i+len(key):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}
