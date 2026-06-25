# Воркфлоу n8n (E4) — сводка

Все файлы — экспортированные n8n workflow (`name`, `nodes`, `connections`, `active`, `settings`).
Импорт: n8n UI → **Import from File**. Все воркфлоу импортируются с `active: false` —
активируйте вручную после проверки.

| Файл | Триггер | Путь вебхука | Вызовы сервисов | Кратко |
| --- | --- | --- | --- | --- |
| `new-lead.json` | webhook POST | `/webhook/new-lead` | CRM `/v1/contacts/upsert`, LLM `/v1/chat`, Channel `/send`, CRM `/v1/leads/upsert` | Новый лид → контакт → автоответ → лид |
| `qualification.json` | webhook POST | `/webhook/qualify` | LLM `/v1/classify`, CRM `/v1/tasks` | Классификация и приоритет по `lead_score` |
| `escalation.json` | webhook POST | `/webhook/escalate` | CRM `/v1/tasks`, CRM `/v1/deals/{id}/stage` | Эскалация на менеджера |
| `follow-up.json` | scheduleTrigger (1 ч) | — | Channel `/send` | Повторное касание при отсутствии ответа |
| `deal-update.json` | webhook POST | `/webhook/crm-update` | CRM `/v1/deals/{id}/stage`, CRM `/v1/notes` | Синхронизация изменений из CRM |

## Ожидаемые форматы входа

- **new-lead / qualification:** Canonical Message (`docs/03-interfaces.md` §0) в теле запроса,
  доступен как `$json.body` (`body.contact`, `body.content.text`, `body.conversation_id`, `body.channel`).
- **escalation:** `{ "conversation_id", "deal_id", "should_escalate": true }`.
- **deal-update:** `{ "deal_id", "stage", "event_type" }`.

## Конвенции

- HTTP Request: `method=POST`, `sendBody=true`, `specifyBody=json`, тело — `jsonBody`
  с выражениями `={{ ... }}`.
- URL внутренних сервисов заданы DNS-именами docker-сети (`http://llm-gateway:8000` и т.д.).
- Динамические сегменты URL (например, `deal_id`) подставляются выражениями в поле URL.
