// Абстракция провайдеров LLM и оркестрация вызовов (эпик E1).
//
// Provider скрывает различия OpenAI / Anthropic / Mock за единым интерфейсом.
// В режиме APP_MODE=mock используется MockProvider (без сети, детерминированно);
// в режиме real вызываются реальные HTTP API с фолбэком на второй провайдер.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"
)

// ChatOpts — параметры одного вызова провайдера.
type ChatOpts struct {
	Model       string
	Temperature float64
	MaxTokens   int
}

// ProviderUsage — счётчики токенов, возвращённые провайдером.
type ProviderUsage struct {
	PromptTokens     int
	CompletionTokens int
}

// Provider — единый интерфейс генерации ответа LLM.
//
// systemPrompt — отрендеренное тело промпта; userMessage — сообщение клиента;
// kbContext — контекст из базы знаний (добавляется в системную часть).
type Provider interface {
	Name() string
	Chat(systemPrompt, userMessage, kbContext string, opts ChatOpts) (text string, usage ProviderUsage, err error)
}

// mockMode сообщает, работает ли сервис в оффлайн-режиме.
func mockMode() bool { return env("APP_MODE", "mock") != "real" }

// providerFamily определяет семейство по префиксу имени модели.
func providerFamily(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(m, "claude"):
		return "anthropic"
	case strings.HasPrefix(m, "gpt"), strings.HasPrefix(m, "o"), strings.HasPrefix(m, "text-"):
		return "openai"
	default:
		return "openai"
	}
}

// providerOrder возвращает порядок попыток: основной провайдер, затем фолбэк.
func providerOrder(model string) []string {
	if providerFamily(model) == "anthropic" {
		return []string{"anthropic", "openai"}
	}
	return []string{"openai", "anthropic"}
}

// newRealProvider создаёт реального провайдера для семейства, если задан ключ.
// Если запрошенная модель не из этого семейства (фолбэк), подбирается модель по умолчанию.
func newRealProvider(family, requested string) (Provider, string, bool) {
	client := &http.Client{Timeout: 30 * time.Second}
	switch family {
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, "", false
		}
		model := requested
		if providerFamily(requested) != "anthropic" {
			model = env("LLM_MODEL_ANTHROPIC", "claude-3-5-haiku-latest")
		}
		return &AnthropicProvider{
			BaseURL: env("ANTHROPIC_BASE_URL", "https://api.anthropic.com"),
			APIKey:  key,
			Client:  client,
		}, model, true
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, "", false
		}
		model := requested
		if providerFamily(requested) != "openai" {
			model = env("LLM_MODEL_CHEAP", "gpt-4o-mini")
		}
		return &OpenAIProvider{
			BaseURL: env("OPENAI_BASE_URL", "https://api.openai.com/v1"),
			APIKey:  key,
			Client:  client,
		}, model, true
	}
	return nil, "", false
}

// generateChat — основная точка вызова: в mock-режиме отдаёт детерминированный
// ответ, в real — пробует основного провайдера, затем фолбэк. Возвращает текст,
// usage и фактически использованную модель.
func generateChat(system, user, kb string, opts ChatOpts) (string, ProviderUsage, string, error) {
	if mockMode() {
		mp := MockProvider{}
		text, usage, err := mp.Chat(system, user, kb, opts)
		return text, usage, opts.Model, err
	}
	lastErr := errors.New("нет настроенного провайдера LLM (проверьте OPENAI_API_KEY / ANTHROPIC_API_KEY)")
	for _, family := range providerOrder(opts.Model) {
		prov, model, ok := newRealProvider(family, opts.Model)
		if !ok {
			continue
		}
		o := opts
		o.Model = model
		text, usage, err := prov.Chat(system, user, kb, o)
		if err == nil {
			return text, usage, model, nil
		}
		lastErr = err
	}
	return "", ProviderUsage{}, opts.Model, lastErr
}

// renderPrompt подставляет variables в тело промпта через text/template.
// При ошибке шаблонизации возвращается исходное тело (без падения запроса).
func renderPrompt(body string, vars map[string]any) string {
	if !strings.Contains(body, "{{") {
		return body
	}
	tmpl, err := template.New("prompt").Option("missingkey=zero").Parse(body)
	if err != nil {
		return body
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, vars); err != nil {
		return body
	}
	return sb.String()
}

// composeSystem добавляет контекст из базы знаний в системную часть.
func composeSystem(system, kb string) string {
	if strings.TrimSpace(kb) == "" {
		return system
	}
	return system + "\n\nКонтекст из базы знаний:\n" + kb
}

// --- MockProvider ---

// MockProvider воспроизводит детерминированное оффлайн-поведение.
type MockProvider struct{}

func (MockProvider) Name() string { return "mock" }

func (MockProvider) Chat(_ /*system*/, user, kb string, _ ChatOpts) (string, ProviderUsage, error) {
	user = strings.TrimSpace(user)
	var text string
	switch {
	case kb != "":
		c := kb
		if len(c) > 280 {
			c = c[:280]
		}
		text = "(черновой ответ по базе знаний) " + c
	case escalate(strings.ToLower(user)):
		text = "Передаю ваш вопрос менеджеру — он скоро свяжется с вами."
	default:
		text = "Спасибо за обращение! Сейчас уточню информацию и вернусь с ответом."
	}
	usage := ProviderUsage{
		PromptTokens:     len(strings.Fields(user)),
		CompletionTokens: len(strings.Fields(text)),
	}
	return text, usage, nil
}

// --- OpenAIProvider ---

// OpenAIProvider вызывает Chat Completions API (OpenAI-совместимые эндпоинты).
type OpenAIProvider struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Chat(system, user, kb string, opts ChatOpts) (string, ProviderUsage, error) {
	payload := map[string]any{
		"model": opts.Model,
		"messages": []map[string]string{
			{"role": "system", "content": composeSystem(system, kb)},
			{"role": "user", "content": user},
		},
		"temperature": opts.Temperature,
		"max_tokens":  opts.MaxTokens,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", ProviderUsage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.client().Do(req)
	if err != nil {
		return "", ProviderUsage{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", ProviderUsage{}, fmt.Errorf("openai: status %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", ProviderUsage{}, fmt.Errorf("openai: bad response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", ProviderUsage{}, errors.New("openai: empty choices")
	}
	usage := ProviderUsage{
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
	}
	return parsed.Choices[0].Message.Content, usage, nil
}

func (p *OpenAIProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// --- AnthropicProvider ---

// AnthropicProvider вызывает Messages API Anthropic.
type AnthropicProvider struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) Chat(system, user, kb string, opts ChatOpts) (string, ProviderUsage, error) {
	payload := map[string]any{
		"model":       opts.Model,
		"max_tokens":  opts.MaxTokens,
		"system":      composeSystem(system, kb),
		"temperature": opts.Temperature,
		"messages": []map[string]string{
			{"role": "user", "content": user},
		},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", ProviderUsage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client().Do(req)
	if err != nil {
		return "", ProviderUsage{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", ProviderUsage{}, fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", ProviderUsage{}, fmt.Errorf("anthropic: bad response: %w", err)
	}
	if len(parsed.Content) == 0 {
		return "", ProviderUsage{}, errors.New("anthropic: empty content")
	}
	usage := ProviderUsage{
		PromptTokens:     parsed.Usage.InputTokens,
		CompletionTokens: parsed.Usage.OutputTokens,
	}
	return parsed.Content[0].Text, usage, nil
}

func (p *AnthropicProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
