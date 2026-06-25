# CRM Connector (E5)

Единый API поверх адаптеров **amoCRM** и **Bitrix24** (решение D1). Контракт — `docs/03-interfaces.md` §4.

Реализация на **Go 1.26** (стандартная библиотека).

Адаптеры:
- `InMemoryAdapter` — mock/dev (`APP_MODE=mock`, без сети).
- `AmoCRMAdapter` — amoCRM REST API v4 (`APP_MODE=real`, `CRM_PROVIDER=amocrm`), Bearer-токен с автообновлением через `oauth2/access_token` при 401.
- `Bitrix24Adapter` — Bitrix24 REST через входящий вебхук (`APP_MODE=real`, `CRM_PROVIDER=bitrix24`).

Выбор провайдера: `CRM_PROVIDER=amocrm|bitrix24`, режим `APP_MODE=mock|real`.

## Запуск
```bash
go run .
go test ./...
```

## Эндпоинты
- `POST /v1/contacts/upsert`, `POST /v1/leads/upsert`
- `POST /v1/deals/{deal_id}/stage`, `POST /v1/tasks`, `POST /v1/notes`
- `GET /v1/contacts/by-phone/{phone}`, `POST /webhooks/crm`

## Переменные окружения
- `APP_MODE` (mock|real), `CRM_PROVIDER` (amocrm|bitrix24)
- amoCRM: `AMOCRM_BASE_URL`, `AMOCRM_ACCESS_TOKEN`, `AMOCRM_REFRESH_TOKEN`, `AMOCRM_CLIENT_ID`, `AMOCRM_CLIENT_SECRET`, `AMOCRM_REDIRECT_URI`, `AMOCRM_PIPELINE_ID`, `AMOCRM_STAGE_MAP`, `AMOCRM_NOTE_ENTITY`
- Bitrix24: `BITRIX24_WEBHOOK_URL`, `BITRIX24_STAGE_MAP`, `BITRIX24_NOTE_ENTITY`
- Прочее: `N8N_WEBHOOK_BASE` (форвард вебхуков), `PORT`

## Маппинг этапов сделок
- **amoCRM** (`UpdateDealStage`): метка воронки → `status_id` через таблицу `defaultAmoStageMap`
  (`new`→142, `in_progress`→143, `won`→142, `lost`→143; заглушки), переопределяется
  `AMOCRM_STAGE_MAP` (JSON `{"метка":"status_id"}`). `pipeline_id` — из `AMOCRM_PIPELINE_ID`.
  Если метка не в таблице и сама число — трактуется как готовый `status_id`. PATCH `/api/v4/leads/{id}`.
- **Bitrix24** (`UpdateDealStage`): метка → `STAGE_ID` напрямую (pass-through), опционально
  через `BITRIX24_STAGE_MAP`. `crm.deal.update`.

## События (log/slog, JSON)
- `lead_created` — при upsert лида; `deal_stage_changed` — при смене этапа; `crm_webhook_received` — на вебхуке.
- TODO: персист событий в БД (нужен SQL-драйвер вне stdlib) — пока только структурированный лог.

## Вебхук `/webhooks/crm`
Минимальный разбор события + best-effort форвард в n8n (`N8N_WEBHOOK_BASE/webhook/crm-update`).
Ошибки форварда не влияют на ответ.
