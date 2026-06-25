// LLM Gateway (E1) — скелет по контракту §2 (docs/03-interfaces.md).
//
// Реализует API-поверхность и базовую логику (тиринг моделей, эвристика эскалации,
// управление промптами). Реальные вызовы OpenAI/Anthropic и чтение промптов из БД
// подключаются в рамках эпика E1 — здесь оставлены явные точки расширения (TODO).
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
	"strings"
	"time"
)

// --- Схемы по контракту §2 ---

type ChatRequest struct {
	PromptID       string         `json:"prompt_id"`
	PromptVersion  string         `json:"prompt_version"`
	Variables      map[string]any `json:"variables"`
	Model          string         `json:"model"`
	ConversationID string         `json:"conversation_id"`
	Temperature    float64        `json:"temperature"`
	MaxTokens      int            `json:"max_tokens"`
}

type Usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

type ChatResponse struct {
	Text           string  `json:"text"`
	ModelUsed      string  `json:"model_used"`
	FinishReason   string  `json:"finish_reason"`
	Confidence     float64 `json:"confidence"`
	ShouldEscalate bool    `json:"should_escalate"`
	Usage          Usage   `json:"usage"`
	TraceID        string  `json:"trace_id"`
}

type ClassifyRequest struct {
	Text           string `json:"text"`
	ConversationID string `json:"conversation_id"`
}

type ClassifyResponse struct {
	Intent    string  `json:"intent"`
	LeadScore float64 `json:"lead_score"`
	TraceID   string  `json:"trace_id"`
}

type PromptInfo struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Version       string         `json:"version"`
	Body          string         `json:"body"`
	ModelDefaults map[string]any `json:"model_defaults"`
}

var escalationKeywords = []string{"менеджер", "человек", "оператор", "жалоба", "manager", "human"}

// Встроенный стартовый набор промптов (в проде — таблица prompts в PostgreSQL).
var prompts = map[string]PromptInfo{
	"sales_assistant": {
		ID:      "sales_assistant",
		Name:    "sales_assistant",
		Version: "v1",
		Body: "Ты — вежливый ассистент отдела продаж Kivano. Отвечай кратко на языке клиента. " +
			"Используй только контекст из базы знаний; если ответа нет — предложи менеджера.",
		ModelDefaults: map[string]any{"temperature": 0.2, "tier": "cheap"},
	},
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// newID — UUID v4 без внешних зависимостей.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// fetchKBContext — сборка контекста из RAG. В mock-режиме допускаем отсутствие RAG.
func fetchKBContext(query string) string {
	if query == "" {
		return ""
	}
	body, _ := json.Marshal(map[string]any{"query": query, "top_k": 5})
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(env("RAG_URL", "http://mock-rag:8000")+"/v1/search", "application/json", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Results []struct {
			Text string `json:"text"`
		} `json:"results"`
	}
	if json.Unmarshal(data, &parsed) != nil {
		return ""
	}
	parts := make([]string, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		parts = append(parts, r.Text)
	}
	return strings.Join(parts, "\n\n")
}

// selectModel — маршрутизация по тирам (решение D4).
func selectModel(requested, tierHint string) string {
	if requested != "" && requested != "auto" {
		return requested
	}
	if tierHint == "strong" {
		return env("LLM_MODEL_STRONG", "gpt-4o")
	}
	return env("LLM_MODEL_CHEAP", "gpt-4o-mini")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func router() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /v1/health", handleHealth)
	mux.HandleFunc("GET /v1/prompts", handleListPrompts)
	mux.HandleFunc("GET /v1/prompts/{id}", handleGetPrompt)
	mux.HandleFunc("POST /v1/chat", handleChat)
	mux.HandleFunc("POST /v1/classify", handleClassify)
	return mux
}

func main() {
	mux := router()
	addr := ":" + env("PORT", "8000")
	log.Printf("llm-gateway listening on %s (mode=%s)", addr, env("APP_MODE", "mock"))
	log.Fatal(http.ListenAndServe(addr, mux))
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "mode": env("APP_MODE", "mock")})
}

func handleListPrompts(w http.ResponseWriter, _ *http.Request) {
	list := make([]PromptInfo, 0, len(prompts))
	for _, p := range prompts {
		list = append(list, p)
	}
	writeJSON(w, http.StatusOK, list)
}

func handleGetPrompt(w http.ResponseWriter, r *http.Request) {
	p, ok := prompts[r.PathValue("id")]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "prompt not found"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	if req.PromptID == "" {
		req.PromptID = "sales_assistant"
	}
	if _, ok := prompts[req.PromptID]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "unknown prompt_id"})
		return
	}

	userMessage, _ := req.Variables["user_message"].(string)
	userMessage = strings.TrimSpace(userMessage)
	lower := strings.ToLower(userMessage)
	shouldEscalate := false
	for _, k := range escalationKeywords {
		if strings.Contains(lower, k) {
			shouldEscalate = true
			break
		}
	}

	kbContext, _ := req.Variables["kb_context"].(string)
	if kbContext == "" {
		kbContext = fetchKBContext(userMessage)
	}

	// TODO(E1): реальный вызов провайдера LLM (OpenAI/Anthropic) с промптом и контекстом.
	var text string
	var confidence float64
	if kbContext != "" {
		if len(kbContext) > 280 {
			kbContext = kbContext[:280]
		}
		text = "(черновой ответ по базе знаний) " + kbContext
		confidence = 0.8
	} else if shouldEscalate {
		text = "Передаю ваш вопрос менеджеру — он скоро свяжется с вами."
		confidence = 0.4
	} else {
		text = "Спасибо за обращение! Сейчас уточню информацию и вернусь с ответом."
		confidence = 0.4
	}

	writeJSON(w, http.StatusOK, ChatResponse{
		Text:           text,
		ModelUsed:      selectModel(req.Model, "cheap"),
		FinishReason:   "stop",
		Confidence:     confidence,
		ShouldEscalate: shouldEscalate || confidence < 0.5,
		Usage:          Usage{PromptTokens: len(strings.Fields(userMessage)), CompletionTokens: len(strings.Fields(text))},
		TraceID:        newID(),
	})
}

func handleClassify(w http.ResponseWriter, r *http.Request) {
	var req ClassifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid json"})
		return
	}
	text := strings.ToLower(req.Text)
	intent, score := "question", 0.5
	switch {
	case containsAny(text, "купить", "цена", "стоит", "заказать", "buy", "price"):
		intent, score = "purchase_intent", 0.8
	case containsAny(text, "привет", "здравствуйте", "hello", "hi"):
		intent, score = "greeting", 0.2
	}
	writeJSON(w, http.StatusOK, ClassifyResponse{Intent: intent, LeadScore: score, TraceID: newID()})
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
