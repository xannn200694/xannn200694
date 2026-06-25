// Контрактная валидация n8n-воркфлоу (E7): каждый JSON в n8n/workflows должен быть
// валидным экспортом n8n (есть name, непустой nodes, присутствует connections).
package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type n8nWorkflow struct {
	Name        string           `json:"name"`
	Nodes       []map[string]any `json:"nodes"`
	Connections map[string]any   `json:"connections"`
}

func TestN8NWorkflowsValid(t *testing.T) {
	dir := filepath.Join("..", "n8n", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("не удалось прочитать %s: %v", dir, err)
	}
	found := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		found++
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var wf n8nWorkflow
		if err := json.Unmarshal(data, &wf); err != nil {
			t.Fatalf("невалидный JSON %s: %v", e.Name(), err)
		}
		if wf.Name == "" {
			t.Errorf("%s: пустое поле name", e.Name())
		}
		if len(wf.Nodes) == 0 {
			t.Errorf("%s: нет нод", e.Name())
		}
		if wf.Connections == nil {
			t.Errorf("%s: отсутствует connections", e.Name())
		}
	}
	if found == 0 {
		t.Fatal("не найдено ни одного воркфлоу n8n")
	}
}
