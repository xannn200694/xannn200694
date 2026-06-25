// CRM Connector (E5) — скелет по контракту §4 (docs/03-interfaces.md).
//
// Единый API поверх адаптеров amoCRM/Bitrix24 (решение D1).
package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

var adapter CRMAdapter

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func router() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /v1/contacts/upsert", handleUpsertContact)
	mux.HandleFunc("POST /v1/leads/upsert", handleUpsertLead)
	mux.HandleFunc("POST /v1/deals/{deal_id}/stage", handleUpdateStage)
	mux.HandleFunc("POST /v1/tasks", handleCreateTask)
	mux.HandleFunc("POST /v1/notes", handleAddNote)
	mux.HandleFunc("GET /v1/contacts/by-phone/{phone}", handleByPhone)
	mux.HandleFunc("POST /webhooks/crm", handleCRMWebhook)
	return mux
}

func main() {
	adapter = getAdapter()
	addr := ":" + env("PORT", "8000")
	log.Printf("crm-connector listening on %s (provider=%s)", addr, adapter.Name())
	log.Fatal(http.ListenAndServe(addr, router()))
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "mode": env("APP_MODE", "mock"), "provider": adapter.Name()})
}

func handleUpsertContact(w http.ResponseWriter, r *http.Request) {
	var c ContactIn
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	writeJSON(w, http.StatusOK, IDResponse{ID: adapter.UpsertContact(c)})
}

func handleUpsertLead(w http.ResponseWriter, r *http.Request) {
	var l LeadIn
	if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	writeJSON(w, http.StatusOK, IDResponse{ID: adapter.UpsertLead(l)})
}

func handleUpdateStage(w http.ResponseWriter, r *http.Request) {
	var b DealStageIn
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	adapter.UpdateDealStage(r.PathValue("deal_id"), b.Stage)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var t TaskIn
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	writeJSON(w, http.StatusOK, IDResponse{ID: adapter.CreateTask(t)})
}

func handleAddNote(w http.ResponseWriter, r *http.Request) {
	var n NoteIn
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	writeJSON(w, http.StatusOK, IDResponse{ID: adapter.AddNote(n)})
}

func handleByPhone(w http.ResponseWriter, r *http.Request) {
	c, ok := adapter.GetContactByPhone(r.PathValue("phone"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "contact not found"})
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func handleCRMWebhook(w http.ResponseWriter, r *http.Request) {
	// TODO(E5): обработка входящих изменений из CRM и проброс в n8n.
	_, _ = io.Copy(io.Discard, r.Body)
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}
