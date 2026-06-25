// Логирование событий вызовов LLM (эпик E1).
//
// EventSink — точка расширения для персиста событий. По умолчанию используется
// StdoutSink (структурный JSON через log/slog). Запись в PostgreSQL (таблица
// events) оставлена как TODO: драйвер БД потребует внешней зависимости, которую
// добавим отдельным шагом.
package main

import (
	"log/slog"
	"os"
	"time"
)

// LLMEvent — событие одного вызова LLM для учёта usage/cost.
type LLMEvent struct {
	ConversationID   string
	Model            string
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
	TraceID          string
	TS               time.Time
}

// EventSink принимает события вызовов LLM.
type EventSink interface {
	Emit(LLMEvent)
}

// StdoutSink пишет событие в stdout структурным JSON через slog.
type StdoutSink struct {
	logger *slog.Logger
}

// NewStdoutSink создаёт sink с JSON-обработчиком slog.
func NewStdoutSink() StdoutSink {
	return StdoutSink{logger: slog.New(slog.NewJSONHandler(os.Stdout, nil))}
}

func (s StdoutSink) Emit(e LLMEvent) {
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("llm_call",
		"event_type", "llm_call",
		"conversation_id", e.ConversationID,
		"model", e.Model,
		"prompt_tokens", e.PromptTokens,
		"completion_tokens", e.CompletionTokens,
		"cost_usd", e.CostUSD,
		"trace_id", e.TraceID,
		"ts", e.TS.UTC().Format(time.RFC3339),
	)
	// TODO(E1): персист события в таблицу events (PostgreSQL) — требует драйвера БД.
}

// eventSink — текущий приёмник событий (переопределяется в тестах).
var eventSink EventSink = NewStdoutSink()
