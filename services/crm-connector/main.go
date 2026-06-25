// CRM Connector (E5) — скелет по контракту §4 (docs/03-interfaces.md).
//
// Единый API поверх адаптеров amoCRM/Bitrix24 (решение D1).
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

var adapter CRMAdapter

// logger пишет структурированные события в JSON (lead_created, deal_stage_changed и пр.).
// TODO(E5): персист событий в БД (нужен SQL-драйвер вне stdlib) — пока только лог.
var logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))

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
	id := adapter.UpsertLead(l)
	logger.Info("lead_created", "lead_id", id, "provider", adapter.Name(), "source", l.Source, "title", l.Title)
	writeJSON(w, http.StatusOK, IDResponse{ID: id})
}

func handleUpdateStage(w http.ResponseWriter, r *http.Request) {
	var b DealStageIn
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	dealID := r.PathValue("deal_id")
	adapter.UpdateDealStage(dealID, b.Stage)
	logger.Info("deal_stage_changed", "deal_id", dealID, "stage", b.Stage, "provider", adapter.Name())
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
	body, _ := io.ReadAll(r.Body)
	event := parseWebhookEvent(body)
	logger.Info("crm_webhook_received", "event", event, "provider", adapter.Name(), "bytes", len(body))
	// Best-effort форвард в n8n; ошибки не влияют на ответ CRM.
	forwardToN8N(body)
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted", "event": event})
}

// parseWebhookEvent минимально извлекает тип события из входящего payload.
// amoCRM/Bitrix24 шлют form-encoded или JSON; пытаемся найти типовые поля.
func parseWebhookEvent(body []byte) string {
	var asJSON map[string]any
	if err := json.Unmarshal(body, &asJSON); err == nil {
		for _, k := range []string{"event", "event_type", "type"} {
			if v, ok := asJSON[k].(string); ok && v != "" {
				return v
			}
		}
	}
	return "unknown"
}

// forwardToN8N best-effort пересылает сырой payload в n8n.
// Если N8N_WEBHOOK_BASE не задан — пропускаем (например, в оффлайн-тестах).
func forwardToN8N(payload []byte) {
	base := env("N8N_WEBHOOK_BASE", "")
	if base == "" {
		return
	}
	url := strings.TrimRight(base, "/") + "/webhook/crm-update"
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		logger.Warn("n8n_forward_build_failed", "error", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("n8n_forward_failed", "error", err.Error())
		return
	}
	_ = resp.Body.Close()
}
