// Контрактная проверка примеров полезной нагрузки (E7): образцы JSON в samples/
// должны содержать обязательные поля по docs/03-interfaces.md.
package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadSample(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("samples", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return m
}

func requireKeys(t *testing.T, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("отсутствует обязательное поле %q", k)
		}
	}
}

func TestCanonicalMessageContract(t *testing.T) {
	m := loadSample(t, "canonical_message.json")
	requireKeys(t, m, "message_id", "channel", "direction", "conversation_id", "contact", "content", "timestamp")
	contact, _ := m["contact"].(map[string]any)
	requireKeys(t, contact, "external_id")
	content, _ := m["content"].(map[string]any)
	requireKeys(t, content, "type")
}

func TestChatResponseContract(t *testing.T) {
	m := loadSample(t, "chat_response.json")
	requireKeys(t, m, "text", "model_used", "finish_reason", "confidence", "should_escalate", "usage", "trace_id")
	usage, _ := m["usage"].(map[string]any)
	requireKeys(t, usage, "prompt_tokens", "completion_tokens", "cost_usd")
}
