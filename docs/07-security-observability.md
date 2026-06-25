# 07. Безопасность и наблюдаемость (E7)

Сводка реализованных контролов и оставшихся задач. Эпик E7 сопровождает все остальные.

## Реализовано

### Безопасность
- **Верификация вебхуков:**
  - WhatsApp Cloud — проверка `X-Hub-Signature-256` (HMAC-SHA256 от сырого тела, ключ `WA_APP_SECRET`).
  - Telegram — проверка `X-Telegram-Bot-Api-Secret-Token` (`TELEGRAM_WEBHOOK_SECRET`).
  - WhatsApp verify-challenge (`hub.verify_token`).
- **Идемпотентность** приёма сообщений (дедуп по `message_id`) — защита от повторной обработки и потери/дублей лидов.
- **Секреты только через окружение** (`.env`), не в репозитории; `.gitignore` исключает `.env` и бинарники.
- **CRM amoCRM** — авто-обновление OAuth access-токена по refresh при 401.

### Наблюдаемость
- **Структурированные события** через `log/slog` (JSON) во всех сервисах: `llm_call`, `message_received`, `message_sent`, `lead_created`, `deal_stage_changed`.
- **trace_id** в ответах LLM Gateway для сквозной трассировки.
- **Healthchecks** у всех сервисов (`/health`) + в `docker-compose` (postgres).
- **Аналитика** (E6) поверх таблицы `events` — витрины KPI и дашборды.

### Тестирование (контракты/качество)
- Юнит/интеграционные тесты в каждом Go-модуле (offline, httptest для внешних API).
- Контрактные тесты в `tests/`: валидация n8n-воркфлоу (структура), проверка обязательных полей образцов payload.
- Smoke-тест живого стека (`KIVANO_SMOKE=1 go test ./...` из `tests/`, либо `scripts/smoke.sh`).
- CI (GitHub Actions): `go vet`/`go build`/`go test` по матрице модулей + `docker compose config`.

## TODO (требуют отдельного решения/доступов)

- **Персист событий в PostgreSQL.** Сейчас события идут в структурированный лог (slog). Запись в таблицу `events`
  требует SQL-драйвера (`pgx`/`lib/pq`) — это первая внешняя зависимость; вынести в отдельное решение (D7) с
  единым пакетом доступа к БД для всех сервисов.
- **Rate limiting** и защита публичных эндпоинтов (на уровне reverse proxy / middleware).
- **Централизованные логи и метрики** (Loki/Prometheus/Grafana), алерты (рост escalation rate, падение capture rate,
  ошибки интеграций, рост стоимости LLM).
- **Бэкапы** PostgreSQL и конфигов n8n + проверка восстановления.
- **PII/комплаенс КГ** — минимизация хранения, шифрование, политика хранения (зависит от ответа по требованиям).
- **Полноценный PDF/OCR** в RAG (вне stdlib).

## Как запускать проверки

```bash
make test                      # go test по всем модулям (offline)
KIVANO_SMOKE=1 go test ./...   # из каталога tests/ — smoke по запущенному стеку
bash scripts/smoke.sh          # curl /health всех сервисов
```
