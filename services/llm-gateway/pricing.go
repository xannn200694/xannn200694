// Подсчёт стоимости вызова LLM по таблице цен (эпик E1).
package main

import "strings"

// modelPrice — цены за 1K токенов (вход/выход) в USD.
type modelPrice struct {
	InputPer1K  float64
	OutputPer1K float64
}

// priceTable — ориентировочные цены известных моделей (per-1K tokens).
// Ключи сопоставляются по префиксу, чтобы покрывать версии вида
// "claude-3-5-sonnet-20241022" или "gpt-4o-2024-08-06".
var priceTable = map[string]modelPrice{
	"gpt-4o-mini":       {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"gpt-4o":            {InputPer1K: 0.005, OutputPer1K: 0.015},
	"claude-3-5-sonnet": {InputPer1K: 0.003, OutputPer1K: 0.015},
	"claude-3-5-haiku":  {InputPer1K: 0.0008, OutputPer1K: 0.004},
}

// lookupPrice ищет цену по точному совпадению, затем по самому длинному префиксу.
func lookupPrice(model string) (modelPrice, bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	if p, ok := priceTable[m]; ok {
		return p, true
	}
	var best string
	for key := range priceTable {
		if strings.HasPrefix(m, key) && len(key) > len(best) {
			best = key
		}
	}
	if best != "" {
		return priceTable[best], true
	}
	return modelPrice{}, false
}

// costUSD считает стоимость вызова; для неизвестных моделей возвращает 0.
func costUSD(model string, usage ProviderUsage) float64 {
	p, ok := lookupPrice(model)
	if !ok {
		return 0
	}
	return float64(usage.PromptTokens)/1000*p.InputPer1K +
		float64(usage.CompletionTokens)/1000*p.OutputPer1K
}
