# Channel Gateway (E3)

Приём/отправка сообщений WhatsApp и Telegram, нормализация в Canonical Message, идемпотентность,
быстрый автоответ. Контракт — `docs/03-interfaces.md` §1.

> Скелет: парсинг вебхуков, нормализация, дедуп, вызов LLM Gateway для автоответа.
> WhatsApp — абстракция провайдера с режимами `cloud_api` и `web_bridge` (решение D2).

## Запуск
```bash
pip install -r requirements.txt
uvicorn app.main:app --reload --port 8003
python -m pytest -q
```

## Эндпоинты
- `POST /webhooks/telegram`
- `GET /webhooks/whatsapp` (verify), `POST /webhooks/whatsapp`
- `POST /send`

## TODO (эпик E3)
- Реальная отправка в Telegram Bot API.
- Реализации WhatsApp: Cloud API (верифиц.) и web-bridge WAHA/Evolution (неверифиц.).
- Верификация подписей вебхуков, шаблоны (HSM), статусы доставки.
- Пересылка в n8n и запись событий `message_received`/`message_sent`.
