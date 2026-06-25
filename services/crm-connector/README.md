# CRM Connector (E5)

Единый API поверх адаптеров **amoCRM** и **Bitrix24** (решение D1). Контракт — `docs/03-interfaces.md` §4.

Реализация на **Go 1.26** (стандартная библиотека).

> Скелет: интерфейс `CRMAdapter` + `InMemoryAdapter` для mock/dev. Реальные адаптеры — в эпике E5.
> Выбор провайдера: `CRM_PROVIDER=amocrm|bitrix24`, режим `APP_MODE=mock|real`.

## Запуск
```bash
go run .
go test ./...
```

## Эндпоинты
- `POST /v1/contacts/upsert`, `POST /v1/leads/upsert`
- `POST /v1/deals/{deal_id}/stage`, `POST /v1/tasks`, `POST /v1/notes`
- `GET /v1/contacts/by-phone/{phone}`, `POST /webhooks/crm`

## TODO (эпик E5)
- amoCRM-адаптер: OAuth 2.0, REST API, маппинг полей/этапов, вебхуки.
- Bitrix24-адаптер: REST через входящий вебхук; учесть коробку (on-prem).
- Идемпотентность/дедуп, запись событий `lead_created`/`deal_stage_changed`.
