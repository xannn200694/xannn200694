package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMockEmbedderDeterministic(t *testing.T) {
	e := NewMockEmbedder()
	a, _ := e.Embed([]string{"доставка по бишкеку"})
	b, _ := e.Embed([]string{"доставка по бишкеку"})
	if len(a) != 1 || len(a[0]) != e.Dim() {
		t.Fatalf("unexpected dim: %d", len(a[0]))
	}
	for i := range a[0] {
		if a[0][i] != b[0][i] {
			t.Fatalf("mock embedder not deterministic at %d", i)
		}
	}
	// Похожие тексты должны быть ближе, чем непохожие.
	q, _ := e.Embed([]string{"сколько идёт доставка"})
	far, _ := e.Embed([]string{"гарантия на технику"})
	if cosine(q[0], a[0]) <= cosine(far[0], a[0]) {
		t.Fatalf("expected delivery query closer to delivery text")
	}
}

func TestOpenAIEmbedder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing/incorrect auth header: %q", r.Header.Get("Authorization"))
		}
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var data []map[string]any
		for i := range req.Input {
			data = append(data, map[string]any{
				"index":     i,
				"embedding": []float32{float32(i) + 0.1, 0.2, 0.3},
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
	}))
	defer srv.Close()

	t.Setenv("OPENAI_BASE_URL", srv.URL)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("EMBEDDINGS_MODEL", "text-embedding-3-small")

	e := NewOpenAIEmbedder()
	vecs, err := e.Embed([]string{"foo", "bar"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 3 {
		t.Fatalf("unexpected vectors: %+v", vecs)
	}
	if vecs[1][0] != 1.1 {
		t.Fatalf("unexpected vector value: %v", vecs[1][0])
	}
}
