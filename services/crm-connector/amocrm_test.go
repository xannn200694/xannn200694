package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAmoServer имитирует минимальный набор эндпоинтов amoCRM v4.
type fakeAmoServer struct {
	nextID    int
	byPhone   map[string]amoContact
	authToken string // требуемый валидный access-токен
	refreshed bool
}

func newFakeAmoServer(authToken string) *fakeAmoServer {
	return &fakeAmoServer{nextID: 100, byPhone: map[string]amoContact{}, authToken: authToken}
}

func (s *fakeAmoServer) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/access_token", func(w http.ResponseWriter, r *http.Request) {
		s.refreshed = true
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":  s.authToken,
			"refresh_token": "new-refresh",
			"expires_in":    86400,
		})
	})

	mux.HandleFunc("GET /api/v4/contacts", func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		phone := r.URL.Query().Get("query")
		c, ok := s.byPhone[phone]
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, amoEmbeddedContacts{Embedded: struct {
			Contacts []amoContact `json:"contacts"`
		}{Contacts: []amoContact{c}}})
	})

	mux.HandleFunc("POST /api/v4/contacts", func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		var in []amoContact
		_ = json.NewDecoder(r.Body).Decode(&in)
		s.nextID++
		c := amoContact{ID: s.nextID, Name: in[0].Name, CustomFieldsValue: in[0].CustomFieldsValue}
		if p := extractPhone(c.CustomFieldsValue); p != "" {
			s.byPhone[p] = c
		}
		writeJSON(w, http.StatusOK, amoEmbeddedContacts{Embedded: struct {
			Contacts []amoContact `json:"contacts"`
		}{Contacts: []amoContact{{ID: c.ID}}}})
	})

	mux.HandleFunc("POST /api/v4/leads", func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		s.nextID++
		writeJSON(w, http.StatusOK, map[string]any{
			"_embedded": map[string]any{"leads": []map[string]any{{"id": s.nextID}}},
		})
	})

	mux.HandleFunc("POST /api/v4/tasks", func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		s.nextID++
		writeJSON(w, http.StatusOK, map[string]any{
			"_embedded": map[string]any{"tasks": []map[string]any{{"id": s.nextID}}},
		})
	})

	return mux
}

func (s *fakeAmoServer) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer "+s.authToken {
		w.WriteHeader(http.StatusUnauthorized)
		return false
	}
	return true
}

func TestAmoCRMUpsertAndLookup(t *testing.T) {
	fake := newFakeAmoServer("valid-token")
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	t.Setenv("AMOCRM_BASE_URL", srv.URL)
	t.Setenv("AMOCRM_ACCESS_TOKEN", "valid-token")
	t.Setenv("AMOCRM_STAGE_MAP", "")

	a := NewAmoCRMAdapter()

	if a.Name() != "amocrm" {
		t.Fatalf("name = %s", a.Name())
	}

	id := a.UpsertContact(ContactIn{Name: "Иван", Phone: "+996700000010"})
	if id == "" {
		t.Fatal("contact not created")
	}
	// Повторный upsert по тому же телефону должен вернуть тот же id (дедуп).
	id2 := a.UpsertContact(ContactIn{Name: "Иван", Phone: "+996700000010"})
	if id2 != id {
		t.Fatalf("dedup failed: %s != %s", id2, id)
	}

	out, ok := a.GetContactByPhone("+996700000010")
	if !ok || out.CRMContactID != id || out.Phone != "+996700000010" {
		t.Fatalf("lookup failed: %+v ok=%v", out, ok)
	}

	leadID := a.UpsertLead(LeadIn{Contact: ContactIn{Name: "Лид", Phone: "+996700000011"}, Title: "Заявка"})
	if leadID == "" {
		t.Fatal("lead not created")
	}

	taskID := a.CreateTask(TaskIn{Text: "Перезвонить", RelatedID: leadID})
	if taskID == "" {
		t.Fatal("task not created")
	}
}

func TestAmoCRMTokenRefreshOn401(t *testing.T) {
	fake := newFakeAmoServer("fresh-token")
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	t.Setenv("AMOCRM_BASE_URL", srv.URL)
	// Стартуем с протухшим токеном — сервер вернёт 401, адаптер обновит токен.
	t.Setenv("AMOCRM_ACCESS_TOKEN", "expired-token")
	t.Setenv("AMOCRM_REFRESH_TOKEN", "some-refresh")
	t.Setenv("AMOCRM_CLIENT_ID", "cid")
	t.Setenv("AMOCRM_CLIENT_SECRET", "secret")

	a := NewAmoCRMAdapter()

	id := a.UpsertContact(ContactIn{Name: "Пётр", Phone: "+996700000012"})
	if id == "" {
		t.Fatal("contact not created after refresh")
	}
	if !fake.refreshed {
		t.Fatal("token refresh was not triggered")
	}
	if a.accessToken != "fresh-token" {
		t.Fatalf("access token not updated: %s", a.accessToken)
	}
}

func TestAmoCRMStageMapping(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	t.Setenv("AMOCRM_BASE_URL", srv.URL)
	t.Setenv("AMOCRM_ACCESS_TOKEN", "valid-token")
	t.Setenv("AMOCRM_STAGE_MAP", `{"won":"555"}`)
	t.Setenv("AMOCRM_PIPELINE_ID", "777")

	a := NewAmoCRMAdapter()
	a.UpdateDealStage("42", "won")

	if !strings.HasSuffix(gotPath, "/api/v4/leads/42") {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotBody["status_id"] != float64(555) {
		t.Fatalf("status_id mapping failed: %v", gotBody["status_id"])
	}
	if gotBody["pipeline_id"] != float64(777) {
		t.Fatalf("pipeline_id failed: %v", gotBody["pipeline_id"])
	}
}
