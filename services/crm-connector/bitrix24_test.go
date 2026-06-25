package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeBitrixServer имитирует REST-методы Bitrix24 (вызовы вида /rest/1/token/{method}.json).
type fakeBitrixServer struct {
	nextID  int
	byPhone map[string]bxContact
	deals   map[string]string // dealID -> STAGE_ID
}

func newFakeBitrixServer() *fakeBitrixServer {
	return &fakeBitrixServer{nextID: 10, byPhone: map[string]bxContact{}, deals: map[string]string{}}
}

func (s *fakeBitrixServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/rest/1/tok/"), ".json")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		switch method {
		case "crm.contact.list":
			phone := ""
			if f, ok := body["filter"].(map[string]any); ok {
				phone, _ = f["PHONE"].(string)
			}
			if c, ok := s.byPhone[phone]; ok {
				writeJSON(w, http.StatusOK, map[string]any{"result": []bxContact{c}})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"result": []bxContact{}})

		case "crm.contact.add":
			s.nextID++
			fields, _ := body["fields"].(map[string]any)
			name, _ := fields["NAME"].(string)
			id := s.nextID
			c := bxContact{ID: itoa(id), Name: name}
			if raw, ok := fields["PHONE"].([]any); ok && len(raw) > 0 {
				if p, ok := raw[0].(map[string]any); ok {
					val, _ := p["VALUE"].(string)
					c.Phone = []bxPhone{{Value: val, ValueType: "WORK"}}
					s.byPhone[val] = c
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"result": id})

		case "crm.lead.add":
			s.nextID++
			writeJSON(w, http.StatusOK, map[string]any{"result": s.nextID})

		case "crm.deal.update":
			id, _ := body["id"].(string)
			if f, ok := body["fields"].(map[string]any); ok {
				stage, _ := f["STAGE_ID"].(string)
				s.deals[id] = stage
			}
			writeJSON(w, http.StatusOK, map[string]any{"result": true})

		default:
			writeJSON(w, http.StatusOK, map[string]any{"result": true})
		}
	})
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return strings.Trim(string(b), `"`)
}

func TestBitrix24UpsertAndLookup(t *testing.T) {
	fake := newFakeBitrixServer()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	t.Setenv("BITRIX24_WEBHOOK_URL", srv.URL+"/rest/1/tok/")
	t.Setenv("BITRIX24_STAGE_MAP", "")

	a := NewBitrix24Adapter()
	if a.Name() != "bitrix24" {
		t.Fatalf("name = %s", a.Name())
	}

	id := a.UpsertContact(ContactIn{Name: "Иван", Phone: "+996700000020"})
	if id == "" {
		t.Fatal("contact not created")
	}
	id2 := a.UpsertContact(ContactIn{Name: "Иван", Phone: "+996700000020"})
	if id2 != id {
		t.Fatalf("dedup failed: %s != %s", id2, id)
	}

	out, ok := a.GetContactByPhone("+996700000020")
	if !ok || out.CRMContactID != id || out.Phone != "+996700000020" {
		t.Fatalf("lookup failed: %+v ok=%v", out, ok)
	}

	leadID := a.UpsertLead(LeadIn{Contact: ContactIn{Name: "Лид", Phone: "+996700000021"}, Title: "Заявка"})
	if leadID == "" {
		t.Fatal("lead not created")
	}
}

func TestBitrix24StageMapping(t *testing.T) {
	fake := newFakeBitrixServer()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	t.Setenv("BITRIX24_WEBHOOK_URL", srv.URL+"/rest/1/tok/")
	t.Setenv("BITRIX24_STAGE_MAP", `{"won":"WON"}`)

	a := NewBitrix24Adapter()
	a.UpdateDealStage("7", "won")

	if fake.deals["7"] != "WON" {
		t.Fatalf("stage mapping failed: %q", fake.deals["7"])
	}
}
