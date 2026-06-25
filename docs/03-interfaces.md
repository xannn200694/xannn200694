# 03. Интерфейсные контракты («швы» для параллельной работы)

> Это **главный документ для параллелизма.** Если все агенты согласуют и реализуют эти контракты,
> они смогут разрабатывать свои компоненты независимо, используя моки/стабы на границах.
> Контракты версионируются (`v1`). Любое изменение — через PR с обновлением этого файла.

## 0. Единый формат сообщения (Canonical Message)

Используется между Channel Gateway, n8n и LLM Gateway.

```json
{
  "message_id": "uuid",
  "channel": "whatsapp | telegram",
  "direction": "inbound | outbound",
  "conversation_id": "string (стабильный ключ диалога)",
  "contact": {
    "external_id": "string (id в канале)",
    "phone": "E.164 | null",
    "username": "string | null",
    "display_name": "string | null"
  },
  "content": {
    "type": "text | image | document | audio | location | template",
    "text": "string | null",
    "media_url": "string | null",
    "payload": {}
  },
  "timestamp": "ISO-8601",
  "metadata": {}
}
```

## 1. Channel Gateway API (E3)

### Входящие вебхуки (от провайдеров → Gateway)
- `POST /webhooks/telegram` — payload Telegram Bot API.
- `POST /webhooks/whatsapp` — payload выбранного провайдера WhatsApp.
- `GET /webhooks/whatsapp` — verification challenge (для Cloud API).

Gateway нормализует payload в **Canonical Message** и передаёт дальше (вызов n8n webhook
или публикация в очередь). Требования: верификация подписи, идемпотентность по `message_id`.

### Исходящие (n8n/сервисы → Gateway → провайдер)
```
POST /send
Body: Canonical Message (direction=outbound)
Resp: { "status": "queued|sent|failed", "provider_message_id": "string|null", "error": "string|null" }
```

## 2. LLM Gateway API (E1)

```
POST /v1/chat
Request:
{
  "prompt_id": "string (имя шаблона промпта)",
  "prompt_version": "string | latest",
  "variables": { "user_message": "...", "kb_context": "...", "history": [...] },
  "model": "auto | gpt-4o | claude-3-5-sonnet | ...",
  "conversation_id": "string",
  "temperature": 0.2,
  "max_tokens": 1024
}
Response:
{
  "text": "string",
  "model_used": "string",
  "finish_reason": "stop|length|...",
  "confidence": 0.0-1.0,
  "should_escalate": true|false,
  "usage": { "prompt_tokens": 0, "completion_tokens": 0, "cost_usd": 0.0 },
  "trace_id": "string"
}
```

Дополнительно:
- `GET /v1/prompts` / `GET /v1/prompts/{id}` — список/получение версий промптов.
- `POST /v1/classify` — намерение/квалификация лида (intent, lead_score).
- Внутренняя логика `should_escalate` (низкая уверенность, явный запрос человека, стоп-слова).

## 3. RAG Service API (E2)

```
POST /v1/ingest
Body: { "documents": [ { "doc_id": "...", "title": "...", "text": "...", "source": "...", "metadata": {} } ] }
Resp: { "ingested": n, "chunks": m }

POST /v1/search
Body: { "query": "string", "top_k": 5, "filters": {} }
Resp: { "results": [ { "chunk_id": "...", "text": "...", "score": 0.0, "source": "...", "metadata": {} } ] }

DELETE /v1/documents/{doc_id}
GET   /v1/health
```

LLM Gateway вызывает `POST /v1/search` для сборки `kb_context`.

## 4. CRM Connector (E5)

Адаптерный интерфейс (одинаковые операции независимо от конкретной CRM):

```
upsert_contact(contact) -> crm_contact_id
upsert_lead(lead) -> crm_lead_id
update_deal_stage(deal_id, stage)
create_task(assignee, due_at, text, related_id)
add_note(entity_id, text)
get_contact_by_phone(phone) -> contact | null
```

Реализуется как набор n8n-нод/HTTP-запросов + тонкий адаптер. Входящие изменения из CRM →
вебхук в n8n (`POST /webhooks/crm`). Маппинг полей CRM фиксируется в `docs/epics/epic-5-crm.md`.

## 5. Схема событий аналитики (E6)

Все компоненты пишут события в таблицу `events` (PostgreSQL) в едином формате:

```json
{
  "event_id": "uuid",
  "event_type": "message_received | message_sent | bot_answered | escalated | lead_created | deal_stage_changed | llm_call",
  "conversation_id": "string|null",
  "contact_external_id": "string|null",
  "channel": "whatsapp|telegram|crm|system",
  "payload": {},
  "ts": "ISO-8601"
}
```

Дашборды строятся поверх `events` (+ витрины/материализованные представления).

## 6. Схема БД (минимум, E0/E6)

```
contacts(id, external_id, channel, phone, username, display_name, crm_contact_id, created_at)
conversations(id, contact_id, channel, status, assigned_to, last_message_at, created_at)
messages(id, conversation_id, direction, content_type, text, provider_message_id, ts)
events(event_id, event_type, conversation_id, contact_external_id, channel, payload jsonb, ts)
prompts(id, name, version, body, model_defaults jsonb, created_at)
```

## 7. Конфигурация и секреты (E0)

Единый `.env.example` со всеми ключами (Telegram token, WhatsApp creds, OpenAI/Anthropic keys,
CRM creds, DB DSN, Qdrant URL). Сервисы читают только свои переменные. Никаких секретов в репозитории.

## 8. Контрактное тестирование

- Для каждого API — OpenAPI-схема + примеры запросов/ответов (в каталоге сервиса).
- Моки границ: `mock-llm`, `mock-rag`, `mock-crm`, `mock-channel` — чтобы эпики тестировались изолированно.
- E7 поддерживает набор контрактных тестов, прогоняемых в CI.
