// Channel Gateway (E3) — скелет по контракту §1 (docs/03-interfaces.md).
//
// Приём вебхуков Telegram/WhatsApp, нормализация в Canonical Message, идемпотентность,
// быстрый автоответ через LLM Gateway. WhatsApp реализован через абстракцию провайдера
// с двумя режимами (cloud_api | web_bridge, решение D2). Реальная отправка и верификация
// подписей подключаются в эпике E3 (TODO).
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

// autoreply — быстрый автоответ через LLM Gateway (best-effort).
func autoreply(msg CanonicalMessage) string {
	body, _ := json.Marshal(map[string]any{
		"conversation_id": msg.ConversationID,
		"variables":       map[string]any{"user_message": msg.Content.Text},
	})
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Post(env("LLM_GATEWAY_URL", "http://mock-llm:8000")+"/v1/chat", "application/json", bytes.NewReader(body))
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
	log.Printf("channel-gateway listening on %s (wa_mode=%s)", addr, env("WA_MODE", "web_bridge"))
	log.Fatal(http.ListenAndServe(addr, router()))
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "mode": env("APP_MODE", "mock"), "wa_mode": env("WA_MODE", "web_bridge")})
}

func handleTelegram(w http.ResponseWriter, r *http.Request) {
	var update map[string]any
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	msg, ok := normalizeTelegram(update)
	if !ok || isDuplicate(msg.MessageID) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	// TODO(E3): отправить reply в Telegram + переслать msg в n8n + записать events.
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted", "conversation_id": msg.ConversationID, "reply": autoreply(msg)})
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
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	msg, ok := normalizeWhatsApp(body)
	if !ok || isDuplicate(msg.MessageID) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted", "conversation_id": msg.ConversationID, "reply": autoreply(msg)})
}

func handleSend(w http.ResponseWriter, r *http.Request) {
	var msg CanonicalMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	// TODO(E3): реальная отправка через Telegram Bot API или WhatsApp-провайдера (WA_MODE).
	writeJSON(w, http.StatusOK, SendResult{Status: "queued", ProviderMessageID: newID()})
}
