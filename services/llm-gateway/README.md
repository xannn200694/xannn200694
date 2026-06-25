# LLM Gateway (E1)

Единая точка доступа к LLM: управление промптами, тиринг моделей (решение D4), сборка контекста из RAG,
эвристика эскалации, учёт стоимости. Контракт — `docs/03-interfaces.md` §2.

Реализация на **Go 1.26** (стандартная библиотека, без внешних зависимостей).

## Запуск (локально)
```bash
go run .            # слушает :8000 (PORT по умолчанию)
go test ./...
```

## Эндпоинты
- `GET /health`, `GET /v1/health`
- `POST /v1/chat`
- `POST /v1/classify`
- `GET /v1/prompts`, `GET /v1/prompts/{id}`

## Провайдеры LLM
- Общий интерфейс `Provider` (`provider.go`): `Chat(system, user, kbContext, opts)`.
- `OpenAIProvider` — `POST {OPENAI_BASE_URL}/chat/completions`, `Authorization: Bearer`.
- `AnthropicProvider` — `POST {ANTHROPIC_BASE_URL}/v1/messages`, `x-api-key` + `anthropic-version`.
- `MockProvider` — детерминированное оффлайн-поведение (`APP_MODE=mock`).
- Семейство по префиксу модели: `gpt`/`o`/`text-` → OpenAI; `claude` → Anthropic.
- Фолбэк: при ошибке основного провайдера в real-режиме пробуется второй (если задан ключ),
  иначе — вежливый ответ с `should_escalate=true`.
- Стоимость (`pricing.go`): таблица per-1K-tokens для известных моделей; неизвестная → 0.
- События (`events.go`): `EventSink`/`StdoutSink` пишет `llm_call` в JSON через `log/slog`.

## TODO (эпик E1)
- Чтение/версионирование промптов из таблицы `prompts` (PostgreSQL) — требует драйвера БД.
- Персист события `llm_call` (usage/cost) в таблицу `events` — `EventSink` уже готов как точка
  расширения; драйвер БД добавим отдельным шагом (внешняя зависимость).
