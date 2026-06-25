// Channel Gateway (E3) — реализация по контракту §1 (docs/03-interfaces.md).
//
// Приём вебхуков Telegram/WhatsApp, нормализация в Canonical Message, идемпотентность,
// быстрый автоответ через LLM Gateway, реальная отправка ответов и верификация подписей.
// WhatsApp реализован через абстракцию провайдера с двумя режимами
// (cloud_api | web_bridge, решение D2). Режим работы — APP_MODE (mock | real).
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// --- Canonical Message (контракт §0) ---

type Contact struct {
	ExternalID  string `json:"external_id"`
	Phone       string `json:"phone,omitempty"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

type Content struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	MediaURL string         `json:"media_url,omitempty"`
	Payload  map[string]any `json:"payload,omitempty"`
}

type CanonicalMessage struct {
	MessageID      string         `json:"message_id"`
	Channel        string         `json:"channel"`
	Direction      string         `json:"direction"`
	ConversationID string         `json:"conversation_id"`
	Contact        Contact        `json:"contact"`
	Content        Content        `json:"content"`
	Timestamp      string         `json:"timestamp"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type SendResult struct {
	Status            string `json:"status"`
	ProviderMessageID string `json:"provider_message_id,omitempty"`
	Error             string `json:"error,omitempty"`
}

var (
	mu      sync.Mutex
	seenIDs = map[string]struct{}{} // защита от дублей вебхуков (в проде — Redis/таблица)

	// Общий HTTP-клиент для исходящих вызовов (Telegram/WhatsApp/n8n/LLM).
	httpClient = &http.Client{Timeout: 8 * time.Second}
)

func init() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func appMode() string { return env("APP_MODE", "mock") }

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }

func isDuplicate(id string) bool {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := seenIDs[id]; ok {
		return true
	}
	seenIDs[id] = struct{}{}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// logEvent — событие аналитики (контракт §5) через slog (JSON).
// TODO(E6): персист в таблицу events (нужен драйвер БД — см. отчёт).
func logEvent(eventType string, msg CanonicalMessage, providerMsgID string) {
	slog.Info(eventType,
		"event_type", eventType,
		"conversation_id", msg.ConversationID,
		"channel", msg.Channel,
		"provider_message_id", providerMsgID,
		"ts", nowISO(),
	)
}

// autoreply — быстрый автоответ через LLM Gateway (best-effort).
func autoreply(msg CanonicalMessage) string {
	body, _ := json.Marshal(map[string]any{
		"conversation_id": msg.ConversationID,
		"variables":       map[string]any{"user_message": msg.Content.Text},
	})
	resp, err := httpClient.Post(env("LLM_GATEWAY_URL", "http://mock-llm:8000")+"/v1/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(data, &parsed)
	return parsed.Text
}

// forwardToN8N — пересылка нормализованного сообщения в n8n (best-effort).
// Ошибки не должны ломать ответ вебхука.
func forwardToN8N(msg CanonicalMessage) {
	base := env("N8N_WEBHOOK_BASE", "")
	if base == "" {
		return
	}
	body, _ := json.Marshal(msg)
	resp, err := httpClient.Post(base+"/webhook/new-lead", "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Warn("n8n forward failed", "error", err.Error())
		return
	}
	_ = resp.Body.Close()
}

// --- Telegram Bot API ---

// sendTelegram отправляет текст через Bot API (sendMessage) и возвращает provider_message_id.
func sendTelegram(chatID, text string) (string, error) {
	base := env("TELEGRAM_BASE_URL", "https://api.telegram.org")
	token := env("TELEGRAM_BOT_TOKEN", "")
	body, _ := json.Marshal(map[string]any{"chat_id": chatID, "text": text})
	resp, err := httpClient.Post(base+"/bot"+token+"/sendMessage", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("telegram api status %d: %s", resp.StatusCode, string(data))
	}
	var parsed struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID any `json:"message_id"`
		} `json:"result"`
	}
	_ = json.Unmarshal(data, &parsed)
	if !parsed.OK {
		return "", fmt.Errorf("telegram api not ok: %s", string(data))
	}
	return numToString(parsed.Result.MessageID), nil
}

// --- Абстракция WhatsApp-провайдера (решение D2) ---

// WAProvider — общий контракт отправки для всех WhatsApp-реализаций.
type WAProvider interface {
	Send(to, text string) (providerMsgID string, err error)
}

// waProvider выбирает реализацию по WA_MODE (cloud_api | web_bridge).
func waProvider() WAProvider {
	switch env("WA_MODE", "web_bridge") {
	case "cloud_api":
		return CloudAPIProvider{}
	default:
		return WebBridgeProvider{}
	}
}

// CloudAPIProvider — официальный WhatsApp Cloud API (верифицированный режим).
type CloudAPIProvider struct{}

func (CloudAPIProvider) Send(to, text string) (string, error) {
	base := env("WA_CLOUD_BASE_URL", "https://graph.facebook.com/v20.0")
	url := base + "/" + env("WA_CLOUD_PHONE_ID", "") + "/messages"
	body, _ := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "text",
		"text":              map[string]any{"body": text},
	})
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env("WA_CLOUD_TOKEN", ""))
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("wa cloud api status %d: %s", resp.StatusCode, string(data))
	}
	var parsed struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(data, &parsed)
	if len(parsed.Messages) > 0 {
		return parsed.Messages[0].ID, nil
	}
	return "", nil
}

// WebBridgeProvider — мост WhatsApp Web (WAHA/Evolution-подобный, неверифицированный режим).
type WebBridgeProvider struct{}

func (WebBridgeProvider) Send(to, text string) (string, error) {
	base := env("WA_BRIDGE_URL", "http://wa-bridge:3000")
	url := base + "/api/sendText"
	body, _ := json.Marshal(map[string]any{
		"session": env("WA_BRIDGE_SESSION", "default"),
		"chatId":  to,
		"text":    text,
	})
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key := env("WA_BRIDGE_API_KEY", ""); key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("wa bridge status %d: %s", resp.StatusCode, string(data))
	}
	// Мосты возвращают разные формы id; пытаемся вытащить, иначе генерируем локальный.
	var parsed struct {
		ID  string `json:"id"`
		Key struct {
			ID string `json:"id"`
		} `json:"key"`
	}
	_ = json.Unmarshal(data, &parsed)
	if parsed.ID != "" {
		return parsed.ID, nil
	}
	if parsed.Key.ID != "" {
		return parsed.Key.ID, nil
	}
	return newID(), nil
}

// --- Верификация подписей вебхуков ---

// verifyTelegram сверяет секретный токен вебхука (если задан TELEGRAM_WEBHOOK_SECRET).
func verifyTelegram(r *http.Request) bool {
	secret := env("TELEGRAM_WEBHOOK_SECRET", "")
	if secret == "" {
		return true
	}
	return r.Header.Get("X-Telegram-Bot-Api-Secret-Token") == secret
}

// verifyWhatsApp сверяет HMAC SHA-256 подпись Cloud API (если задан WA_APP_SECRET).
func verifyWhatsApp(r *http.Request, raw []byte) bool {
	secret := env("WA_APP_SECRET", "")
	if secret == "" {
		return true
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	got := r.Header.Get("X-Hub-Signature-256")
	return hmac.Equal([]byte(expected), []byte(got))
}

// --- Нормализация ---

func normalizeTelegram(update map[string]any) (CanonicalMessage, bool) {
	message, ok := update["message"].(map[string]any)
	if !ok {
		message, ok = update["edited_message"].(map[string]any)
		if !ok {
			return CanonicalMessage{}, false
		}
	}
	chat, _ := message["chat"].(map[string]any)
	from, _ := message["from"].(map[string]any)
	chatID := numToString(chat["id"])
	text, _ := message["text"].(string)
	return CanonicalMessage{
		MessageID:      fmt.Sprintf("tg-%s-%s", numToString(message["message_id"]), chatID),
		Channel:        "telegram",
		Direction:      "inbound",
		ConversationID: chatID,
		Contact: Contact{
			ExternalID:  numToString(from["id"]),
			Username:    asString(from["username"]),
			DisplayName: asString(from["first_name"]),
		},
		Content:   Content{Type: "text", Text: text},
		Timestamp: nowISO(),
	}, true
}

func normalizeWhatsApp(body map[string]any) (CanonicalMessage, bool) {
	entry, ok := body["entry"].([]any)
	if !ok || len(entry) == 0 {
		return CanonicalMessage{}, false
	}
	e0, _ := entry[0].(map[string]any)
	changes, ok := e0["changes"].([]any)
	if !ok || len(changes) == 0 {
		return CanonicalMessage{}, false
	}
	c0, _ := changes[0].(map[string]any)
	value, _ := c0["value"].(map[string]any)
	msgs, ok := value["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return CanonicalMessage{}, false
	}
	m0, _ := msgs[0].(map[string]any)
	from := asString(m0["from"])
	var text string
	if t, ok := m0["text"].(map[string]any); ok {
		text = asString(t["body"])
	}
	return CanonicalMessage{
		MessageID:      "wa-" + asString(m0["id"]),
		Channel:        "whatsapp",
		Direction:      "inbound",
		ConversationID: from,
		Contact:        Contact{ExternalID: from, Phone: from},
		Content:        Content{Type: "text", Text: text},
		Timestamp:      nowISO(),
	}, true
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func numToString(v any) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatInt(int64(n), 10)
	case json.Number:
		return n.String()
	case string:
		return n
	default:
		return ""
	}
}

func router() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /webhooks/telegram", handleTelegram)
	mux.HandleFunc("GET /webhooks/whatsapp", handleWhatsAppVerify)
	mux.HandleFunc("POST /webhooks/whatsapp", handleWhatsApp)
	mux.HandleFunc("POST /send", handleSend)
	return mux
}

func main() {
	addr := ":" + env("PORT", "8000")
	log.Printf("channel-gateway listening on %s (app_mode=%s wa_mode=%s)", addr, appMode(), env("WA_MODE", "web_bridge"))
	log.Fatal(http.ListenAndServe(addr, router()))
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "mode": appMode(), "wa_mode": env("WA_MODE", "web_bridge")})
}

func handleTelegram(w http.ResponseWriter, r *http.Request) {
	if !verifyTelegram(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "invalid secret token"})
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var update map[string]any
	if err := json.Unmarshal(raw, &update); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	msg, ok := normalizeTelegram(update)
	if !ok || isDuplicate(msg.MessageID) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	logEvent("message_received", msg, "")
	forwardToN8N(msg)
	reply := autoreply(msg)
	if appMode() == "real" && reply != "" {
		if pmid, err := sendTelegram(msg.ConversationID, reply); err != nil {
			slog.Error("telegram send failed", "conversation_id", msg.ConversationID, "error", err.Error())
		} else {
			logEvent("message_sent", CanonicalMessage{Channel: "telegram", ConversationID: msg.ConversationID}, pmid)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted", "conversation_id": msg.ConversationID, "reply": reply})
}

func handleWhatsAppVerify(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("hub.verify_token") == env("WA_CLOUD_VERIFY_TOKEN", "verify_me") {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(q.Get("hub.challenge")))
		return
	}
	w.WriteHeader(http.StatusForbidden)
}

func handleWhatsApp(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	if !verifyWhatsApp(r, raw) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "invalid signature"})
		return
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	msg, ok := normalizeWhatsApp(body)
	if !ok || isDuplicate(msg.MessageID) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	logEvent("message_received", msg, "")
	forwardToN8N(msg)
	reply := autoreply(msg)
	if appMode() == "real" && reply != "" {
		if pmid, err := waProvider().Send(msg.ConversationID, reply); err != nil {
			slog.Error("whatsapp send failed", "conversation_id", msg.ConversationID, "error", err.Error())
		} else {
			logEvent("message_sent", CanonicalMessage{Channel: "whatsapp", ConversationID: msg.ConversationID}, pmid)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted", "conversation_id": msg.ConversationID, "reply": reply})
}

func handleSend(w http.ResponseWriter, r *http.Request) {
	var msg CanonicalMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	// В mock-режиме сеть не трогаем — сообщение «поставлено в очередь».
	if appMode() != "real" {
		writeJSON(w, http.StatusOK, SendResult{Status: "queued", ProviderMessageID: newID()})
		return
	}
	var (
		pmid string
		err  error
	)
	switch msg.Channel {
	case "telegram":
		pmid, err = sendTelegram(msg.ConversationID, msg.Content.Text)
	case "whatsapp":
		to := msg.Contact.Phone
		if to == "" {
			to = msg.ConversationID
		}
		pmid, err = waProvider().Send(to, msg.Content.Text)
	default:
		writeJSON(w, http.StatusBadRequest, SendResult{Status: "failed", Error: "unknown channel: " + msg.Channel})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, SendResult{Status: "failed", Error: err.Error()})
		return
	}
	logEvent("message_sent", msg, pmid)
	writeJSON(w, http.StatusOK, SendResult{Status: "sent", ProviderMessageID: pmid})
}
