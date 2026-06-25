// RAG Service (E2) — скелет по контракту §3 (docs/03-interfaces.md).
//
// Хранилище и поиск реализованы в памяти с наивным лексическим скорингом, чтобы каркас
// работал без внешних зависимостей. В эпике E2 заменяется на эмбеддинги + Qdrant/pgvector,
// а ингест расширяется парсерами сайта/Word/PDF (решение D5).
package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const chunkSize = 500

type Document struct {
	DocID    string         `json:"doc_id"`
	Title    string         `json:"title"`
	Text     string         `json:"text"`
	Source   string         `json:"source"`
	Metadata map[string]any `json:"metadata"`
}

type IngestRequest struct {
	Documents []Document `json:"documents"`
}

type IngestResponse struct {
	Ingested int `json:"ingested"`
	Chunks   int `json:"chunks"`
}

type SearchRequest struct {
	Query   string         `json:"query"`
	TopK    int            `json:"top_k"`
	Filters map[string]any `json:"filters"`
}

type SearchResult struct {
	ChunkID  string         `json:"chunk_id"`
	Text     string         `json:"text"`
	Score    float64        `json:"score"`
	Source   string         `json:"source"`
	Metadata map[string]any `json:"metadata"`
}

type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

type chunk struct {
	text     string
	source   string
	metadata map[string]any
	docID    string
}

var (
	mu    sync.RWMutex
	index = map[string]chunk{} // chunk_id -> chunk
	wsRe  = regexp.MustCompile(`\s+`)
	wRe   = regexp.MustCompile(`[\p{L}\p{N}_]+`) // Unicode-aware (включая кириллицу)
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

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

func chunkText(text string) []string {
	text = strings.TrimSpace(wsRe.ReplaceAllString(text, " "))
	if text == "" {
		return []string{""}
	}
	runes := []rune(text)
	var out []string
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}

func tokenize(s string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, t := range wRe.FindAllString(strings.ToLower(s), -1) {
		set[t] = struct{}{}
	}
	return set
}

func indexDocument(doc Document) int {
	// Идемпотентный ингест: удаляем прежние чанки документа.
	for cid, c := range index {
		if c.docID == doc.DocID {
			delete(index, cid)
		}
	}
	source := doc.Source
	if source == "" {
		source = doc.Title
	}
	chunks := chunkText(doc.Text)
	for _, ch := range chunks {
		meta := map[string]any{"title": doc.Title}
		for k, v := range doc.Metadata {
			meta[k] = v
		}
		index[newID()] = chunk{text: ch, source: source, metadata: meta, docID: doc.DocID}
	}
	return len(chunks)
}

func router() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /v1/health", handleHealth)
	mux.HandleFunc("POST /v1/ingest", handleIngest)
	mux.HandleFunc("POST /v1/search", handleSearch)
	mux.HandleFunc("DELETE /v1/documents/{doc_id}", handleDelete)
	return mux
}

func main() {
	addr := ":" + env("PORT", "8000")
	log.Printf("rag listening on %s (mode=%s)", addr, env("APP_MODE", "mock"))
	log.Fatal(http.ListenAndServe(addr, router()))
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	mu.RLock()
	docs := map[string]struct{}{}
	for _, c := range index {
		docs[c.docID] = struct{}{}
	}
	mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "mode": env("APP_MODE", "mock"), "docs": len(docs)})
}

func handleIngest(w http.ResponseWriter, r *http.Request) {
	var req IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	mu.Lock()
	total := 0
	for _, d := range req.Documents {
		total += indexDocument(d)
	}
	mu.Unlock()
	writeJSON(w, http.StatusOK, IngestResponse{Ingested: len(req.Documents), Chunks: total})
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}
	qTokens := tokenize(req.Query)
	var results []SearchResult
	mu.RLock()
	for cid, c := range index {
		ct := tokenize(c.text)
		overlap := 0
		for t := range qTokens {
			if _, ok := ct[t]; ok {
				overlap++
			}
		}
		if overlap == 0 {
			continue
		}
		denom := len(qTokens)
		if denom == 0 {
			denom = 1
		}
		results = append(results, SearchResult{
			ChunkID:  cid,
			Text:     c.text,
			Score:    float64(overlap) / float64(denom),
			Source:   c.source,
			Metadata: c.metadata,
		})
	}
	mu.RUnlock()
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > req.TopK {
		results = results[:req.TopK]
	}
	writeJSON(w, http.StatusOK, SearchResponse{Results: results})
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	docID := r.PathValue("doc_id")
	mu.Lock()
	deleted := 0
	for cid, c := range index {
		if c.docID == docID {
			delete(index, cid)
			deleted++
		}
	}
	mu.Unlock()
	if deleted == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "document not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted_chunks": deleted})
}
