package main

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func doJSON(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	router().ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	rec := doJSON(t, http.MethodGet, "/health", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestListPrompts(t *testing.T) {
	rec := doJSON(t, http.MethodGet, "/v1/prompts", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "sales_assistant") {
		t.Fatalf("unexpected: %d %s", rec.Code, rec.Body.String())
	}
}

func TestChatBasic(t *testing.T) {
	rec := doJSON(t, http.MethodPost, "/v1/chat", `{"variables":{"user_message":"Здравствуйте"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "trace_id") {
		t.Fatalf("no trace_id: %s", rec.Body.String())
	}
}

func TestChatEscalation(t *testing.T) {
	rec := doJSON(t, http.MethodPost, "/v1/chat", `{"variables":{"user_message":"хочу менеджера"}}`)
	if !strings.Contains(rec.Body.String(), `"should_escalate":true`) {
		t.Fatalf("expected escalation: %s", rec.Body.String())
	}
}

func TestClassify(t *testing.T) {
	rec := doJSON(t, http.MethodPost, "/v1/classify", `{"text":"сколько стоит?"}`)
	if !strings.Contains(rec.Body.String(), "purchase_intent") {
		t.Fatalf("expected purchase_intent: %s", rec.Body.String())
	}
}

// --- Выбор провайдера по имени модели ---

func TestProviderFamily(t *testing.T) {
	cases := map[string]string{
		"gpt-4o":                  "openai",
		"gpt-4o-mini":             "openai",
		"o1-mini":                 "openai",
		"text-davinci-003":        "openai",
		"claude-3-5-sonnet":       "anthropic",
		"claude-3-5-haiku-latest": "anthropic",
		"some-unknown-model":      "openai",
	}
	for model, want := range cases {
		if got := providerFamily(model); got != want {
			t.Errorf("providerFamily(%q)=%q, want %q", model, got, want)
		}
	}
}

func TestProviderOrder(t *testing.T) {
	if got := providerOrder("claude-3-5-haiku"); got[0] != "anthropic" || got[1] != "openai" {
		t.Errorf("anthropic-first order broken: %v", got)
	}
	if got := providerOrder("gpt-4o"); got[0] != "openai" || got[1] != "anthropic" {
		t.Errorf("openai-first order broken: %v", got)
	}
}

// --- Подсчёт стоимости ---

func TestCostUSD(t *testing.T) {
	// gpt-4o: 0.005/1K in, 0.015/1K out. 1000 in + 1000 out = 0.02
	got := costUSD("gpt-4o", ProviderUsage{PromptTokens: 1000, CompletionTokens: 1000})
	if math.Abs(got-0.02) > 1e-9 {
		t.Errorf("gpt-4o cost=%v, want 0.02", got)
	}
	// Версионированная модель сопоставляется по префиксу.
	got = costUSD("claude-3-5-sonnet-20241022", ProviderUsage{PromptTokens: 1000, CompletionTokens: 0})
	if math.Abs(got-0.003) > 1e-9 {
		t.Errorf("claude sonnet cost=%v, want 0.003", got)
	}
	// Неизвестная модель — 0.
	if got := costUSD("unknown-xyz", ProviderUsage{PromptTokens: 5000, CompletionTokens: 5000}); got != 0 {
		t.Errorf("unknown model cost=%v, want 0", got)
	}
}

// --- Real-режим: OpenAI через httptest ---

func TestChatRealOpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("bad auth header: %q", auth)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "gpt-4o-mini") {
			t.Errorf("model not in body: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"Привет из OpenAI!"}}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`)
	}))
	defer srv.Close()

	t.Setenv("APP_MODE", "real")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", srv.URL)

	rec := doJSON(t, http.MethodPost, "/v1/chat", `{"model":"gpt-4o-mini","variables":{"user_message":"привет","kb_context":"тариф базовый"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp ChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Text != "Привет из OpenAI!" {
		t.Errorf("text=%q", resp.Text)
	}
	if resp.Usage.PromptTokens != 11 || resp.Usage.CompletionTokens != 7 {
		t.Errorf("usage=%+v", resp.Usage)
	}
	// cost = 11/1000*0.00015 + 7/1000*0.0006
	wantCost := 11.0/1000*0.00015 + 7.0/1000*0.0006
	if math.Abs(resp.Usage.CostUSD-wantCost) > 1e-9 {
		t.Errorf("cost=%v, want %v", resp.Usage.CostUSD, wantCost)
	}
	if resp.ModelUsed != "gpt-4o-mini" {
		t.Errorf("model_used=%q", resp.ModelUsed)
	}
}

// --- Real-режим: Anthropic через httptest ---

func TestChatRealAnthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "ak" {
			t.Errorf("bad x-api-key: %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("bad anthropic-version: %q", r.Header.Get("anthropic-version"))
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"content":[{"text":"Ответ Claude"}],"usage":{"input_tokens":5,"output_tokens":3}}`)
	}))
	defer srv.Close()

	t.Setenv("APP_MODE", "real")
	t.Setenv("ANTHROPIC_API_KEY", "ak")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)

	rec := doJSON(t, http.MethodPost, "/v1/chat", `{"model":"claude-3-5-haiku","variables":{"user_message":"привет"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp ChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Text != "Ответ Claude" {
		t.Errorf("text=%q", resp.Text)
	}
	if resp.Usage.PromptTokens != 5 || resp.Usage.CompletionTokens != 3 {
		t.Errorf("usage=%+v", resp.Usage)
	}
}

// --- Real-режим: фолбэк с OpenAI на Anthropic при ошибке основного ---

func TestChatRealFallback(t *testing.T) {
	oai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":"boom"}`)
	}))
	defer oai.Close()
	anth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"content":[{"text":"фолбэк сработал"}],"usage":{"input_tokens":2,"output_tokens":2}}`)
	}))
	defer anth.Close()

	t.Setenv("APP_MODE", "real")
	t.Setenv("OPENAI_API_KEY", "k1")
	t.Setenv("ANTHROPIC_API_KEY", "k2")
	t.Setenv("OPENAI_BASE_URL", oai.URL)
	t.Setenv("ANTHROPIC_BASE_URL", anth.URL)

	rec := doJSON(t, http.MethodPost, "/v1/chat", `{"model":"gpt-4o","variables":{"user_message":"привет"}}`)
	var resp ChatResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Text != "фолбэк сработал" {
		t.Fatalf("expected fallback text, got %q (model=%s)", resp.Text, resp.ModelUsed)
	}
}

// --- Real-режим: нет провайдеров — вежливая эскалация ---

func TestChatRealNoProvider(t *testing.T) {
	t.Setenv("APP_MODE", "real")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	rec := doJSON(t, http.MethodPost, "/v1/chat", `{"model":"gpt-4o","variables":{"user_message":"привет"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"should_escalate":true`) {
		t.Fatalf("expected escalation: %s", rec.Body.String())
	}
}

// --- Real-режим: LLM-классификация через httptest ---

func TestClassifyRealLLM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"intent\":\"complaint\",\"lead_score\":0.2}"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`)
	}))
	defer srv.Close()

	t.Setenv("APP_MODE", "real")
	t.Setenv("OPENAI_API_KEY", "k")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", srv.URL)

	rec := doJSON(t, http.MethodPost, "/v1/classify", `{"text":"верните деньги"}`)
	if !strings.Contains(rec.Body.String(), "complaint") {
		t.Fatalf("expected complaint from LLM: %s", rec.Body.String())
	}
}
