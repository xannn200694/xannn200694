// Абстракция векторного хранилища (эпик E2).
//
// MemoryStore — in-memory индекс с лексическим скорингом (APP_MODE=mock и фолбэк).
// QdrantStore — реальное хранилище через HTTP REST к Qdrant (APP_MODE=real).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// chunkData — единица индексации: текст фрагмента, его вектор и метаданные.
type chunkData struct {
	ChunkID  string
	Text     string
	Source   string
	Metadata map[string]any
	DocID    string
	Vector   []float32
}

// Store — общий интерфейс векторного/лексического хранилища.
type Store interface {
	// IndexDocument идемпотентно заменяет все чанки документа docID.
	IndexDocument(docID string, chunks []chunkData) error
	// Search возвращает top_k наиболее релевантных чанков под запрос.
	// vec — вектор запроса (используется векторными хранилищами); query — исходный текст
	// (используется лексическим MemoryStore).
	Search(query string, vec []float32, topK int, filters map[string]any) ([]SearchResult, error)
	// Delete удаляет все чанки документа, возвращает их число.
	Delete(docID string) (int, error)
	// Docs — количество уникальных документов (для health).
	Docs() int
}

// --- MemoryStore ------------------------------------------------------------

// MemoryStore хранит чанки в памяти и ранжирует их лексически (пересечение токенов).
type MemoryStore struct {
	mu    sync.RWMutex
	index map[string]chunkData // chunk_id -> chunk
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{index: map[string]chunkData{}}
}

func (s *MemoryStore) IndexDocument(docID string, chunks []chunkData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for cid, c := range s.index {
		if c.DocID == docID {
			delete(s.index, cid)
		}
	}
	for _, ch := range chunks {
		s.index[ch.ChunkID] = ch
	}
	return nil
}

func (s *MemoryStore) Search(query string, _ []float32, topK int, filters map[string]any) ([]SearchResult, error) {
	qTokens := tokenize(query)
	denom := len(qTokens)
	if denom == 0 {
		denom = 1
	}
	var results []SearchResult
	s.mu.RLock()
	for cid, c := range s.index {
		if !matchFilters(c, filters) {
			continue
		}
		ct := tokenize(c.Text)
		overlap := 0
		for t := range qTokens {
			if _, ok := ct[t]; ok {
				overlap++
			}
		}
		if overlap == 0 {
			continue
		}
		results = append(results, SearchResult{
			ChunkID:  cid,
			Text:     c.Text,
			Score:    float64(overlap) / float64(denom),
			Source:   c.Source,
			Metadata: c.Metadata,
		})
	}
	s.mu.RUnlock()
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func (s *MemoryStore) Delete(docID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := 0
	for cid, c := range s.index {
		if c.DocID == docID {
			delete(s.index, cid)
			deleted++
		}
	}
	return deleted, nil
}

func (s *MemoryStore) Docs() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	docs := map[string]struct{}{}
	for _, c := range s.index {
		docs[c.DocID] = struct{}{}
	}
	return len(docs)
}

// matchFilters проверяет соответствие чанка фильтрам (по source и ключам metadata).
func matchFilters(c chunkData, filters map[string]any) bool {
	for k, want := range filters {
		if want == nil {
			continue
		}
		var got any
		if k == "source" {
			got = c.Source
		} else if v, ok := c.Metadata[k]; ok {
			got = v
		} else {
			return false
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			return false
		}
	}
	return true
}

// --- QdrantStore ------------------------------------------------------------

// QdrantStore работает с Qdrant по HTTP REST.
type QdrantStore struct {
	baseURL    string
	collection string
	dim        int
	client     *http.Client
	once       sync.Once
	mu         sync.Mutex
	docs       map[string]struct{} // локальный учёт документов для health
}

func NewQdrantStore(dim int) *QdrantStore {
	return &QdrantStore{
		baseURL:    env("QDRANT_URL", "http://qdrant:6333"),
		collection: env("RAG_COLLECTION", "k-technology_kb"),
		dim:        dim,
		client:     &http.Client{Timeout: 30 * time.Second},
		docs:       map[string]struct{}{},
	}
}

func (s *QdrantStore) ensureCollection() error {
	body, _ := json.Marshal(map[string]any{
		"vectors": map[string]any{"size": s.dim, "distance": "Cosine"},
	})
	req, err := http.NewRequest(http.MethodPut, s.url("/collections/"+s.collection), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	// 200 — создано; 409/иные с уже существующей коллекцией считаем успехом.
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusConflict {
		return nil
	}
	return fmt.Errorf("qdrant create collection: status %d: %s", resp.StatusCode, string(data))
}

func (s *QdrantStore) IndexDocument(docID string, chunks []chunkData) error {
	var initErr error
	s.once.Do(func() { initErr = s.ensureCollection() })
	if initErr != nil {
		return initErr
	}
	if err := s.deleteByDoc(docID); err != nil {
		return err
	}
	points := make([]map[string]any, 0, len(chunks))
	for _, ch := range chunks {
		payload := map[string]any{
			"text":   ch.Text,
			"source": ch.Source,
			"doc_id": ch.DocID,
		}
		for k, v := range ch.Metadata {
			payload[k] = v
		}
		points = append(points, map[string]any{
			"id":      ch.ChunkID,
			"vector":  ch.Vector,
			"payload": payload,
		})
	}
	body, _ := json.Marshal(map[string]any{"points": points})
	req, err := http.NewRequest(http.MethodPut, s.url("/collections/"+s.collection+"/points?wait=true"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qdrant upsert: status %d: %s", resp.StatusCode, string(data))
	}
	s.mu.Lock()
	s.docs[docID] = struct{}{}
	s.mu.Unlock()
	return nil
}

func (s *QdrantStore) deleteByDoc(docID string) error {
	body, _ := json.Marshal(map[string]any{
		"filter": qdrantDocFilter(docID),
	})
	req, err := http.NewRequest(http.MethodPost, s.url("/collections/"+s.collection+"/points/delete?wait=true"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qdrant delete: status %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

func (s *QdrantStore) Search(_ string, vec []float32, topK int, filters map[string]any) ([]SearchResult, error) {
	if topK <= 0 {
		topK = 5
	}
	reqBody := map[string]any{
		"vector":       vec,
		"limit":        topK,
		"with_payload": true,
	}
	if f := qdrantFilter(filters); f != nil {
		reqBody["filter"] = f
	}
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, s.url("/collections/"+s.collection+"/points/search"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qdrant search: status %d: %s", resp.StatusCode, string(data))
	}
	var parsed struct {
		Result []struct {
			ID      any            `json:"id"`
			Score   float64        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(parsed.Result))
	for _, p := range parsed.Result {
		text, _ := p.Payload["text"].(string)
		source, _ := p.Payload["source"].(string)
		meta := map[string]any{}
		for k, v := range p.Payload {
			if k == "text" || k == "source" {
				continue
			}
			meta[k] = v
		}
		results = append(results, SearchResult{
			ChunkID:  fmt.Sprint(p.ID),
			Text:     text,
			Score:    p.Score,
			Source:   source,
			Metadata: meta,
		})
	}
	return results, nil
}

func (s *QdrantStore) Delete(docID string) (int, error) {
	var initErr error
	s.once.Do(func() { initErr = s.ensureCollection() })
	if initErr != nil {
		return 0, initErr
	}
	if err := s.deleteByDoc(docID); err != nil {
		return 0, err
	}
	s.mu.Lock()
	_, existed := s.docs[docID]
	delete(s.docs, docID)
	s.mu.Unlock()
	if existed {
		return 1, nil
	}
	return 0, nil
}

func (s *QdrantStore) Docs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.docs)
}

func (s *QdrantStore) url(path string) string { return s.baseURL + path }

func qdrantDocFilter(docID string) map[string]any {
	return map[string]any{
		"must": []any{
			map[string]any{"key": "doc_id", "match": map[string]any{"value": docID}},
		},
	}
}

// qdrantFilter преобразует пользовательские фильтры в формат Qdrant (must match value).
func qdrantFilter(filters map[string]any) map[string]any {
	if len(filters) == 0 {
		return nil
	}
	var must []any
	for k, v := range filters {
		if v == nil {
			continue
		}
		must = append(must, map[string]any{"key": k, "match": map[string]any{"value": v}})
	}
	if len(must) == 0 {
		return nil
	}
	return map[string]any{"must": must}
}

// newStore выбирает реализацию по APP_MODE; dim — размерность векторов эмбеддера.
func newStore(dim int) Store {
	if env("APP_MODE", "mock") == "real" {
		return NewQdrantStore(dim)
	}
	return NewMemoryStore()
}
