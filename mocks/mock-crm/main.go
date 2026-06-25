// Мок CRM Connector (контракт §4). Возвращает фиктивные идентификаторы.
package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func id(val string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, map[string]string{"id": val}) }
}

func router() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"status": "ok", "mock": true, "provider": "mock"})
	})
	mux.HandleFunc("POST /v1/contacts/upsert", id("mock-contact-1"))
	mux.HandleFunc("POST /v1/leads/upsert", id("mock-lead-1"))
	mux.HandleFunc("POST /v1/tasks", id("mock-task-1"))
	mux.HandleFunc("POST /v1/notes", id("mock-note-1"))
	mux.HandleFunc("POST /v1/deals/{deal_id}/stage", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/contacts/by-phone/{phone}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"crm_contact_id": "mock-contact-1", "phone": r.PathValue("phone")})
	})
	return mux
}

func main() {
	log.Println("mock-crm listening on :8000")
	log.Fatal(http.ListenAndServe(":8000", router()))
}
