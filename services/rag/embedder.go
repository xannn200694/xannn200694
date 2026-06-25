// Абстракция эмбеддингов (эпик E2).
//
// MockEmbedder — детерминированные векторы из хеша токенов, чтобы поиск работал
// оффлайн (APP_MODE=mock). OpenAIEmbedder — реальные эмбеддинги через OpenAI-совместимый
// HTTP API (APP_MODE=real).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"time"
)

// Embedder превращает тексты в плотные векторы фиксированной размерности.
type Embedder interface {
	// Embed возвращает по одному вектору на каждый входной текст.
	Embed(texts []string) ([][]float32, error)
	// Dim — размерность вектора (нужна для создания коллекции в Qdrant).
	Dim() int
}

// --- MockEmbedder -----------------------------------------------------------

const mockEmbedDim = 256

// MockEmbedder строит детерминированный bag-of-words вектор: каждый токен хешируется
// в индекс измерения, значения нормируются. Косинусная близость таких векторов
// коррелирует с пересечением токенов, поэтому семантический поиск работает оффлайн.
type MockEmbedder struct {
	dim int
}

func NewMockEmbedder() *MockEmbedder { return &MockEmbedder{dim: mockEmbedDim} }

func (e *MockEmbedder) Dim() int { return e.dim }

func (e *MockEmbedder) Embed(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = e.vector(t)
	}
	return out, nil
}

func (e *MockEmbedder) vector(text string) []float32 {
	vec := make([]float32, e.dim)
	for tok := range tokenize(text) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(tok))
		sum := h.Sum32()
		idx := int(sum % uint32(e.dim))
		// Знак из старшего бита, чтобы значения распределялись в обе стороны.
		if sum&0x80000000 != 0 {
			vec[idx] -= 1
		} else {
			vec[idx] += 1
		}
	}
	return l2normalize(vec)
}

func l2normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	norm := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= norm
	}
	return v
}

// --- OpenAIEmbedder ---------------------------------------------------------

// OpenAIEmbedder вызывает OpenAI-совместимый эндпоинт /embeddings.
// OPENAI_BASE_URL переопределяем (нужно для тестов и self-hosted шлюзов).
type OpenAIEmbedder struct {
	baseURL string
	apiKey  string
	model   string
	dim     int
	client  *http.Client
}

func NewOpenAIEmbedder() *OpenAIEmbedder {
	model := env("EMBEDDINGS_MODEL", "text-embedding-3-small")
	return &OpenAIEmbedder{
		baseURL: env("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		apiKey:  env("OPENAI_API_KEY", ""),
		model:   model,
		dim:     defaultModelDim(model),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// defaultModelDim — известные размерности OpenAI-моделей (для создания коллекции).
func defaultModelDim(model string) int {
	switch model {
	case "text-embedding-3-large":
		return 3072
	case "text-embedding-ada-002", "text-embedding-3-small":
		return 1536
	default:
		return 1536
	}
}

func (e *OpenAIEmbedder) Dim() int { return e.dim }

func (e *OpenAIEmbedder) Embed(texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	reqBody, _ := json.Marshal(map[string]any{"model": e.model, "input": texts})
	req, err := http.NewRequest(http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai embeddings: status %d: %s", resp.StatusCode, string(data))
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	out := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index >= 0 && d.Index < len(out) {
			out[d.Index] = d.Embedding
		}
	}
	for i := range out {
		if out[i] == nil {
			return nil, fmt.Errorf("openai embeddings: missing vector for input %d", i)
		}
		if e.dim == 0 || e.dim != len(out[i]) {
			e.dim = len(out[i])
		}
	}
	return out, nil
}

// newEmbedder выбирает реализацию по APP_MODE.
func newEmbedder() Embedder {
	if env("APP_MODE", "mock") == "real" {
		return NewOpenAIEmbedder()
	}
	return NewMockEmbedder()
}
