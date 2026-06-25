# Channel Gateway (E3)

Приём/отправка сообщений WhatsApp и Telegram, нормализация в Canonical Message, идемпотентность,
быстрый автоответ. Контракт — `docs/03-interfaces.md` §1.

Реализация на **Go 1.26** (стандартная библиотека).

> Скелет: парсинг вебхуков, нормализация, дедуп, вызов LLM Gateway для автоответа.
> WhatsApp — абстракция провайдера с режимами `cloud_api` и `web_bridge` (решение D2).

## Запуск
```bash
go run .
go test ./...
```

## Эндпоинты
- `POST /webhooks/telegram`
- `GET /webhooks/whatsapp` (verify), `POST /webhooks/whatsapp`
- `POST /send`

## Режимы (APP_MODE)
- `mock` (по умолчанию) — оффлайн, `/send` возвращает `queued` без сети.
- `real` — реальные вызовы Telegram Bot API / WhatsApp-провайдера.

## Реализовано (эпик E3)
- Реальная отправка в Telegram Bot API (`sendMessage`), база переопределяется `TELEGRAM_BASE_URL`.
- WhatsApp через абстракцию `WAProvider` с выбором по `WA_MODE`:
  - `cloud_api` — WhatsApp Cloud API (`WA_CLOUD_BASE_URL`, Bearer-токен);
  - `web_bridge` — мост WAHA/Evolution-подобный (`WA_BRIDGE_URL`, `X-Api-Key`).
- Верификация подписей вебхуков: Telegram secret-token (`X-Telegram-Bot-Api-Secret-Token`),
  WhatsApp Cloud HMAC SHA-256 (`X-Hub-Signature-256`).
- Пересылка нормализованного сообщения в n8n (`POST $N8N_WEBHOOK_BASE/webhook/new-lead`, best-effort).
- События `message_received` / `message_sent` через `log/slog` (JSON).
- `/send` диспетчеризует по `channel`.

## TODO
- Персист событий/сообщений в PostgreSQL (`events`, `messages`) — нужен драйвер БД (вне stdlib).
- Шаблоны (HSM) для исходящих вне 24-часового окна (Cloud API), статусы доставки и ретраи.
- Медиа (image/document/audio) в нормализации и отправке.
