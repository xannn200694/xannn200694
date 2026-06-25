# n8n — оркестрация воронки (E4)

Self-hosted n8n запускается через `docker-compose.yml` (порт 5678). Воркфлоу версионируются
в каталоге `workflows/` (экспорт JSON) для ревью и переноса между средами. Каталог монтируется
в контейнер как `/workflows` (см. `docker-compose.yml`, сервис `n8n`).

## Воркфлоу (эпик E4)

| Файл | Триггер | Путь вебхука | Назначение |
| --- | --- | --- | --- |
| `workflows/new-lead.json` | Webhook (POST) | `/webhook/new-lead` | Новый лид: upsert контакта → автоответ LLM → отправка → upsert лида. |
| `workflows/qualification.json` | Webhook (POST) | `/webhook/qualify` | Классификация (`/v1/classify`), приоритет по `lead_score`, задача по горячим лидам. |
| `workflows/escalation.json` | Webhook (POST) | `/webhook/escalate` | По флагу `should_escalate`: задача менеджеру, уведомление, смена этапа сделки. |
| `workflows/follow-up.json` | Schedule (1 час) | — | Заглушка выборки «нет ответа» → follow-up через `/send`. |
| `workflows/deal-update.json` | Webhook (POST) | `/webhook/crm-update` | Изменение из CRM: смена этапа сделки и/или заметка. |

### Потоки по шагам

- **new-lead** — `Webhook New Lead` → `Upsert Contact` (`POST /v1/contacts/upsert`) →
  `LLM Chat` (`POST /v1/chat`, `variables.user_message`) → `Send Reply` (`POST /send`) →
  `Upsert Lead` (`POST /v1/leads/upsert`). Вход — Canonical Message в `body`.
- **qualification** — `Classify` (`POST /v1/classify`) → `Is Hot` (`lead_score >= 70`) →
  ветви приоритета high/medium/low; для горячих — `Create Hot Lead Task` (`POST /v1/tasks`).
- **escalation** — `Should Escalate` (IF по `body.should_escalate`) →
  `Create Manager Task` (`POST /v1/tasks`) → `Build Notification` (Set) →
  `Update Deal Stage` (`POST /v1/deals/{deal_id}/stage`).
- **follow-up** — `Every Hour` (scheduleTrigger) → `Stub No-Reply Leads` (Set, заглушка
  выборки диалогов без ответа N часов) → `Send Follow-up` (`POST /send`).
- **deal-update** — `Stage Provided` (IF по `body.stage`) → при наличии этапа
  `Update Deal Stage` (`POST /v1/deals/{deal_id}/stage`) → `Add Note` (`POST /v1/notes`).

## Внешние сервисы (в docker-сети)

- LLM Gateway — `http://llm-gateway:8000` (`POST /v1/chat`, `POST /v1/classify`).
- Channel Gateway — `http://channel-gateway:8000` (`POST /send`).
- CRM Connector — `http://crm-connector:8000` (`/v1/contacts/upsert`, `/v1/leads/upsert`,
  `/v1/deals/{id}/stage`, `/v1/tasks`, `/v1/notes`).

## Импорт/экспорт

- **Импорт:** в UI n8n → меню воркфлоу → **Import from File** → выбрать JSON из `workflows/`.
  После импорта проверьте/назначьте креденшелы (если потребуются) и активируйте воркфлоу.
- **Экспорт:** в UI n8n → Workflow → **Download** → сохранить JSON в `workflows/` (для ревью/версионирования).

## Пути вебхуков, которые ожидают смежные сервисы

- `channel-gateway` шлёт входящие лиды на `POST /webhook/new-lead`.
- `crm-connector` шлёт изменения из CRM на `POST /webhook/crm-update`.
- Вспомогательные (для ручного/межворкфлоу вызова): `POST /webhook/qualify`, `POST /webhook/escalate`.

> Примечание: в тестовом режиме n8n пути имеют префикс `/webhook-test/...`. В production —
> `/webhook/...`.
