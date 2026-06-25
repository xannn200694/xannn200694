// RAG Service (E2) — реализация по контракту §3 (docs/03-interfaces.md).
//
// Архитектура:
//   - Embedder (embedder.go): MockEmbedder (оффлайн) | OpenAIEmbedder (real).
//   - Store (store.go): MemoryStore (лексический, оффлайн/фолбэк) | QdrantStore (real).
//   - Парсеры источников (parsers.go): веб-сайт, Word (.docx), PDF (решение D5).
//
// APP_MODE=mock (по умолчанию) работает полностью оффлайн; APP_MODE=real ходит в
// OpenAI Embeddings и Qdrant.
package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
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

var (
	wsRe = regexp.MustCompile(`\s+`)
	wRe  = regexp.MustCompile(`[\p{L}\p{N}_]+`) // Unicode-aware (включая кириллицу)
)

// Активные бэкенды. Инициализируются по APP_MODE; тесты могут переопределять напрямую.
var (
	activeEmbedder = newEmbedder()
	activeStore    = newStore(activeEmbedder.Dim())
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

// detectLang — best-effort определение языка по алфавиту (ru/en), иначе "".
func detectLang(s string) string {
	var cyr, lat int
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			cyr++
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			lat++
		}
	}
	switch {
	case cyr == 0 && lat == 0:
		return ""
	case cyr >= lat:
		return "ru"
	default:
		return "en"
	}
}

// ingestDocument чанкует документ, считает эмбеддинги и пишет в хранилище.
// Возвращает количество созданных чанков.
func ingestDocument(doc Document) (int, error) {
	if doc.DocID == "" {
		doc.DocID = newID()
	}
	source := doc.Source
	if source == "" {
		source = doc.Title
	}
	chunks := chunkText(doc.Text)
	vectors, err := activeEmbedder.Embed(chunks)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	cds := make([]chunkData, 0, len(chunks))
	for i, ch := range chunks {
		meta := map[string]any{
			"title":      doc.Title,
			"source":     source,
			"updated_at": now,
		}
		if lang := detectLang(ch); lang != "" {
			meta["lang"] = lang
		}
		for k, v := range doc.Metadata {
			meta[k] = v
		}
		var vec []float32
		if i < len(vectors) {
			vec = vectors[i]
		}
		cds = append(cds, chunkData{
			ChunkID:  newID(),
			Text:     ch,
			Source:   source,
			Metadata: meta,
			DocID:    doc.DocID,
			Vector:   vec,
		})
	}
	if err := activeStore.IndexDocument(doc.DocID, cds); err != nil {
		return 0, err
	}
	return len(cds), nil
}

func router() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /v1/health", handleHealth)
	mux.HandleFunc("POST /v1/ingest", handleIngest)
	mux.HandleFunc("POST /v1/ingest/web", handleIngestWeb)
	mux.HandleFunc("POST /v1/ingest/docx", handleIngestDocx)
	mux.HandleFunc("POST /v1/ingest/pdf", handleIngestPDF)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"mode":   env("APP_MODE", "mock"),
		"docs":   activeStore.Docs(),
	})
}

func handleIngest(w http.ResponseWriter, r *http.Request) {
	var req IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	total := 0
	for _, d := range req.Documents {
		n, err := ingestDocument(d)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"detail": "ingest failed: " + err.Error()})
			return
		}
		total += n
	}
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
	var vec []float32
	if vecs, err := activeEmbedder.Embed([]string{req.Query}); err == nil && len(vecs) > 0 {
		vec = vecs[0]
	} else if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"detail": "embed failed: " + err.Error()})
		return
	}
	results, err := activeStore.Search(req.Query, vec, req.TopK, req.Filters)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"detail": "search failed: " + err.Error()})
		return
	}
	if results == nil {
		results = []SearchResult{}
	}
	writeJSON(w, http.StatusOK, SearchResponse{Results: results})
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	docID := r.PathValue("doc_id")
	deleted, err := activeStore.Delete(docID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"detail": "delete failed: " + err.Error()})
		return
	}
	if deleted == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "document not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted_chunks": deleted})
}
