// Мок LLM Gateway (контракт §2). Детерминированные ответы для изоляции эпиков.
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

func router() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /v1/health", health)
	mux.HandleFunc("POST /v1/chat", chat)
	mux.HandleFunc("POST /v1/classify", classify)
	mux.HandleFunc("GET /v1/prompts", prompts)
	return mux
}

func main() {
	log.Println("mock-llm listening on :8000")
	log.Fatal(http.ListenAndServe(":8000", router()))
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"status": "ok", "mock": true})
}

func chat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Variables map[string]any `json:"variables"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	user, _ := req.Variables["user_message"].(string)
	writeJSON(w, map[string]any{
		"text":            "[mock-llm] Получено: " + user,
		"model_used":      "mock-model",
		"finish_reason":   "stop",
		"confidence":      0.9,
		"should_escalate": false,
		"usage":           map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "cost_usd": 0.0},
		"trace_id":        "mock-trace",
	})
}

func classify(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"intent": "question", "lead_score": 0.5, "trace_id": "mock-trace"})
}

func prompts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, []map[string]any{{"id": "sales_assistant", "name": "sales_assistant", "version": "v1", "body": "mock"}})
}
