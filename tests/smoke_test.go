// Smoke-тест живого стека (E7). Пропускается, если KIVANO_SMOKE != "1".
// Запуск после `docker compose up`:
//
//	KIVANO_SMOKE=1 go test ./...
package tests

import (
	"net/http"
	"os"
	"testing"
	"time"
)

func TestSmokeHealth(t *testing.T) {
	if os.Getenv("KIVANO_SMOKE") != "1" {
		t.Skip("smoke отключён (установите KIVANO_SMOKE=1 при запущенном docker compose)")
	}
	endpoints := map[string]string{
		"llm-gateway":     env("LLM_GATEWAY_URL", "http://localhost:8001") + "/health",
		"rag":             env("RAG_URL", "http://localhost:8002") + "/health",
		"channel-gateway": env("CHANNEL_GATEWAY_URL", "http://localhost:8003") + "/health",
		"crm-connector":   env("CRM_CONNECTOR_URL", "http://localhost:8004") + "/health",
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for name, url := range endpoints {
		resp, err := client.Get(url)
		if err != nil {
			t.Errorf("%s недоступен (%s): %v", name, url, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s вернул %d", name, resp.StatusCode)
		}
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
