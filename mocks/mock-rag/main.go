// Мок RAG Service (контракт §3). Возвращает фиксированный фрагмент знаний.
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
	mux.HandleFunc("POST /v1/ingest", ingest)
	mux.HandleFunc("POST /v1/search", search)
	return mux
}

func main() {
	log.Println("mock-rag listening on :8000")
	log.Fatal(http.ListenAndServe(":8000", router()))
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"status": "ok", "mock": true})
}

func ingest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Documents []any `json:"documents"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	writeJSON(w, map[string]any{"ingested": len(req.Documents), "chunks": len(req.Documents)})
}

func search(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query string `json:"query"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	writeJSON(w, map[string]any{
		"results": []map[string]any{{
			"chunk_id": "mock-chunk-1",
			"text":     "[mock-rag] Релевантный фрагмент для запроса: " + req.Query,
			"score":    0.99,
			"source":   "mock-kb",
			"metadata": map[string]any{"title": "mock"},
		}},
	})
}
