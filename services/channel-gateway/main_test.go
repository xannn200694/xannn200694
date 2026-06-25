package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func do(method, path, body string) *httptest.ResponseRecorder {
	return doWithHeaders(method, path, body, nil)
}

func doWithHeaders(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router().ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	if do(http.MethodGet, "/health", "").Code != http.StatusOK {
		t.Fatal("health failed")
	}
}

func TestTelegramWebhook(t *testing.T) {
	update := `{"message":{"message_id":10,"chat":{"id":555},"from":{"id":777,"username":"user","first_name":"Иван"},"text":"Здравствуйте"}}`
	rec := do(http.MethodPost, "/webhooks/telegram", update)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "accepted") {
		t.Fatalf("telegram failed: %s", rec.Body.String())
	}
	if rec2 := do(http.MethodPost, "/webhooks/telegram", update); !strings.Contains(rec2.Body.String(), "ignored") {
		t.Fatalf("expected duplicate ignored: %s", rec2.Body.String())
	}
}

func TestWhatsAppVerify(t *testing.T) {
	rec := do(http.MethodGet, "/webhooks/whatsapp?hub.verify_token=verify_me&hub.challenge=12345", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "12345" {
		t.Fatalf("verify failed: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSend(t *testing.T) {
	msg := `{"message_id":"m1","channel":"telegram","direction":"outbound","conversation_id":"555","contact":{"external_id":"777"},"content":{"type":"text","text":"Привет"},"timestamp":"2026-01-01T00:00:00Z"}`
	rec := do(http.MethodPost, "/send", msg)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "queued") {
		t.Fatalf("send failed: %s", rec.Body.String())
	}
}

// Реальная отправка Telegram: httptest вместо api.telegram.org + замоканный LLM Gateway.
func TestTelegramRealSend(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"text":"Здравствуйте! Чем помочь?"}`))
	}))
	defer llm.Close()

	var gotSendMessage bool
	var gotPayload string
	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			gotSendMessage = true
			b, _ := io.ReadAll(r.Body)
			gotPayload = string(b)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":4242}}`))
	}))
	defer tg.Close()

	t.Setenv("APP_MODE", "real")
	t.Setenv("LLM_GATEWAY_URL", llm.URL)
	t.Setenv("TELEGRAM_BASE_URL", tg.URL)
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")

	update := `{"message":{"message_id":20,"chat":{"id":999},"from":{"id":888,"username":"u2","first_name":"Пётр"},"text":"Привет"}}`
	rec := do(http.MethodPost, "/webhooks/telegram", update)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "accepted") {
		t.Fatalf("telegram real failed: %d %s", rec.Code, rec.Body.String())
	}
	if !gotSendMessage {
		t.Fatal("expected sendMessage to be called")
	}
	if !strings.Contains(gotPayload, "999") {
		t.Fatalf("expected chat_id in payload: %s", gotPayload)
	}
}

func TestSendTelegramReal(t *testing.T) {
	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7}}`))
	}))
	defer tg.Close()
	t.Setenv("APP_MODE", "real")
	t.Setenv("TELEGRAM_BASE_URL", tg.URL)

	msg := `{"channel":"telegram","direction":"outbound","conversation_id":"111","content":{"type":"text","text":"Hi"}}`
	rec := do(http.MethodPost, "/send", msg)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"sent"`) || !strings.Contains(rec.Body.String(), "7") {
		t.Fatalf("send telegram real failed: %d %s", rec.Code, rec.Body.String())
	}
}

func TestWACloudAPIProvider(t *testing.T) {
	var auth, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.ABC"}]}`))
	}))
	defer srv.Close()
	t.Setenv("WA_CLOUD_BASE_URL", srv.URL)
	t.Setenv("WA_CLOUD_PHONE_ID", "PHONE1")
	t.Setenv("WA_CLOUD_TOKEN", "cloud-token")

	id, err := CloudAPIProvider{}.Send("79990001122", "Привет")
	if err != nil {
		t.Fatalf("cloud send err: %v", err)
	}
	if id != "wamid.ABC" {
		t.Fatalf("unexpected id: %s", id)
	}
	if auth != "Bearer cloud-token" {
		t.Fatalf("unexpected auth: %s", auth)
	}
	if !strings.Contains(body, `"messaging_product":"whatsapp"`) || !strings.Contains(body, "79990001122") {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestWAWebBridgeProvider(t *testing.T) {
	var apiKey, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey = r.Header.Get("X-Api-Key")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"id":"bridge-123"}`))
	}))
	defer srv.Close()
	t.Setenv("WA_BRIDGE_URL", srv.URL)
	t.Setenv("WA_BRIDGE_API_KEY", "bridge-key")

	id, err := WebBridgeProvider{}.Send("79990001122", "Привет")
	if err != nil {
		t.Fatalf("bridge send err: %v", err)
	}
	if id != "bridge-123" {
		t.Fatalf("unexpected id: %s", id)
	}
	if apiKey != "bridge-key" {
		t.Fatalf("unexpected api key: %s", apiKey)
	}
	if !strings.Contains(body, "79990001122") {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestWASelectByMode(t *testing.T) {
	t.Setenv("WA_MODE", "cloud_api")
	if _, ok := waProvider().(CloudAPIProvider); !ok {
		t.Fatal("expected CloudAPIProvider")
	}
	t.Setenv("WA_MODE", "web_bridge")
	if _, ok := waProvider().(WebBridgeProvider); !ok {
		t.Fatal("expected WebBridgeProvider")
	}
}

func waSignature(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWhatsAppSignatureValid(t *testing.T) {
	t.Setenv("WA_APP_SECRET", "topsecret")
	body := `{"entry":[{"changes":[{"value":{"messages":[{"id":"wamidVALID","from":"79990001122","text":{"body":"Привет"}}]}}]}]}`
	rec := doWithHeaders(http.MethodPost, "/webhooks/whatsapp", body, map[string]string{
		"X-Hub-Signature-256": waSignature("topsecret", body),
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "accepted") {
		t.Fatalf("valid signature failed: %d %s", rec.Code, rec.Body.String())
	}
}

func TestWhatsAppSignatureInvalid(t *testing.T) {
	t.Setenv("WA_APP_SECRET", "topsecret")
	body := `{"entry":[{"changes":[{"value":{"messages":[{"id":"wamidBAD","from":"79990001122","text":{"body":"Привет"}}]}}]}]}`
	rec := doWithHeaders(http.MethodPost, "/webhooks/whatsapp", body, map[string]string{
		"X-Hub-Signature-256": "sha256=deadbeef",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for invalid signature, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestTelegramSecretToken(t *testing.T) {
	t.Setenv("TELEGRAM_WEBHOOK_SECRET", "abc123")
	update := `{"message":{"message_id":30,"chat":{"id":1001},"from":{"id":1,"first_name":"A"},"text":"hi"}}`
	if rec := doWithHeaders(http.MethodPost, "/webhooks/telegram", update, map[string]string{
		"X-Telegram-Bot-Api-Secret-Token": "wrong",
	}); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for wrong token, got %d", rec.Code)
	}
	if rec := doWithHeaders(http.MethodPost, "/webhooks/telegram", update, map[string]string{
		"X-Telegram-Bot-Api-Secret-Token": "abc123",
	}); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "accepted") {
		t.Fatalf("expected accepted for valid token, got %d %s", rec.Code, rec.Body.String())
	}
}
