package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type evalSet struct {
	TopK      int        `json:"top_k"`
	Documents []Document `json:"documents"`
	Questions []struct {
		Query          string   `json:"query"`
		ExpectedSource string   `json:"expected_source"`
		Keywords       []string `json:"keywords"`
	} `json:"questions"`
}

// TestRecallAtK проверяет качество поиска на оценочном наборе (MockEmbedder + MemoryStore).
func TestRecallAtK(t *testing.T) {
	data, err := os.ReadFile("eval/questions.json")
	if err != nil {
		t.Fatalf("read eval set: %v", err)
	}
	var set evalSet
	if err := json.Unmarshal(data, &set); err != nil {
		t.Fatalf("parse eval set: %v", err)
	}
	if set.TopK <= 0 {
		set.TopK = 3
	}

	// Изолированное хранилище, чтобы не зависеть от других тестов.
	prev := activeStore
	activeStore = NewMemoryStore()
	defer func() { activeStore = prev }()

	for _, d := range set.Documents {
		if _, err := ingestDocument(d); err != nil {
			t.Fatalf("ingest %s: %v", d.DocID, err)
		}
	}

	hits := 0
	for _, q := range set.Questions {
		results, err := activeStore.Search(q.Query, nil, set.TopK, nil)
		if err != nil {
			t.Fatalf("search %q: %v", q.Query, err)
		}
		found := false
		for _, r := range results {
			if r.Source == q.ExpectedSource {
				found = true
				break
			}
		}
		if found {
			hits++
		} else {
			t.Logf("miss: query=%q expected_source=%q keywords=%v", q.Query, q.ExpectedSource, strings.Join(q.Keywords, ","))
		}
	}

	recall := float64(hits) / float64(len(set.Questions))
	t.Logf("recall@%d = %.2f (%d/%d)", set.TopK, recall, hits, len(set.Questions))
	const target = 0.8
	if recall < target {
		t.Fatalf("recall@%d=%.2f below target %.2f", set.TopK, recall, target)
	}
}
