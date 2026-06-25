package main

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

// mockQdrant имитирует нужные эндпоинты Qdrant в памяти.
type mockQdrantPoint struct {
	id      any
	vector  []float32
	payload map[string]any
}

func newMockQdrant() http.Handler {
	points := map[string]mockQdrantPoint{}
	mux := http.NewServeMux()

	mux.HandleFunc("PUT /collections/{name}", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"result": true, "status": "ok"})
	})

	mux.HandleFunc("PUT /collections/{name}/points", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Points []struct {
				ID      any            `json:"id"`
				Vector  []float32      `json:"vector"`
				Payload map[string]any `json:"payload"`
			} `json:"points"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, p := range body.Points {
			points[toKey(p.ID)] = mockQdrantPoint{id: p.ID, vector: p.Vector, payload: p.Payload}
		}
		writeJSON(w, http.StatusOK, map[string]any{"result": map[string]any{"status": "completed"}, "status": "ok"})
	})

	mux.HandleFunc("POST /collections/{name}/points/delete", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Filter struct {
				Must []struct {
					Key   string `json:"key"`
					Match struct {
						Value any `json:"value"`
					} `json:"match"`
				} `json:"must"`
			} `json:"filter"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for key, p := range points {
			match := true
			for _, cond := range body.Filter.Must {
				if toKey(p.payload[cond.Key]) != toKey(cond.Match.Value) {
					match = false
					break
				}
			}
			if match {
				delete(points, key)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"result": map[string]any{"status": "completed"}, "status": "ok"})
	})

	mux.HandleFunc("POST /collections/{name}/points/search", func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var body struct {
			Vector []float32 `json:"vector"`
			Limit  int       `json:"limit"`
		}
		_ = json.Unmarshal(data, &body)
		type scored struct {
			id      any
			score   float64
			payload map[string]any
		}
		var all []scored
		for _, p := range points {
			all = append(all, scored{id: p.id, score: cosine(body.Vector, p.vector), payload: p.payload})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].score > all[j].score })
		if body.Limit > 0 && len(all) > body.Limit {
			all = all[:body.Limit]
		}
		result := make([]map[string]any, 0, len(all))
		for _, s := range all {
			result = append(result, map[string]any{"id": s.id, "score": s.score, "payload": s.payload})
		}
		writeJSON(w, http.StatusOK, map[string]any{"result": result, "status": "ok"})
	})

	return mux
}

func toKey(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func TestQdrantStoreUpsertSearch(t *testing.T) {
	srv := httptest.NewServer(newMockQdrant())
	defer srv.Close()

	t.Setenv("QDRANT_URL", srv.URL)
	t.Setenv("RAG_COLLECTION", "test_kb")

	prev := activeStore
	activeStore = NewQdrantStore(activeEmbedder.Dim())
	defer func() { activeStore = prev }()

	docs := `{"documents":[
		{"doc_id":"q1","title":"Доставка","text":"Доставка по Бишкеку занимает один рабочий день.","source":"site"},
		{"doc_id":"q2","title":"Оплата","text":"Оплата возможна картой Visa и наличными.","source":"site"}
	]}`
	if rec := do(http.MethodPost, "/v1/ingest", docs); rec.Code != http.StatusOK {
		t.Fatalf("qdrant ingest failed: %d %s", rec.Code, rec.Body.String())
	}

	rec := do(http.MethodPost, "/v1/search", `{"query":"сколько идёт доставка по бишкеку","top_k":2}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("qdrant search failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp SearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Fatalf("expected results, got none: %s", rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(resp.Results[0].Text), "доставка") {
		t.Fatalf("top result not about delivery: %+v", resp.Results[0])
	}

	// Удаление документа.
	if rec := do(http.MethodDelete, "/v1/documents/q1", ""); rec.Code != http.StatusOK {
		t.Fatalf("qdrant delete failed: %d %s", rec.Code, rec.Body.String())
	}
}
