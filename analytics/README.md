# Аналитика и дашборды (E6)

Сквозная аналитика по лидам, ответам и сделкам платформы K-Technology. Строится поверх
таблиц `contacts / conversations / messages / events` (контракт §6,
[`../infra/db/init.sql`](../infra/db/init.sql)) и набора KPI-витрин.

Metabase и PostgreSQL поднимаются в общем `docker-compose.yml` (Metabase — порт 3000,
в одной сети с `postgres`).

## Структура каталога

| Путь | Назначение |
|---|---|
| [`sql/`](sql/) | KPI-витрины `CREATE OR REPLACE VIEW` (префикс `kpi_`). |
| [`sql/00_apply.sql`](sql/00_apply.sql) | Подключает все витрины по порядку через `\ir`. |
| [`seed/seed_events.sql`](seed/seed_events.sql) | Идемпотентный генератор демо-данных за ~14 дней. |
| [`dashboards/README.md`](dashboards/README.md) | Карточки Metabase, подключение к БД, экспорт/импорт. |

## Витрины (`sql/`)

| Файл | Витрины | Что считает | Источники |
|---|---|---|---|
| `ttfr.sql` | `kpi_ttfr_per_conversation`, `kpi_ttfr_daily`, `kpi_ttfr_daily_total` | Time To First Response: время между первым inbound и первым outbound; медиана (p50) и p95 по дням (`percentile_cont`). | `messages`, `conversations` |
| `lead_capture.sql` | `kpi_lead_capture_daily`, `kpi_lead_capture_by_channel` | Входящие диалоги/уникальные контакты и лиды по дням/каналам; `lead_capture_rate = lead_created / unique_contacts`. | `conversations`, `events` |
| `automation_rate.sql` | `kpi_conversation_automation`, `kpi_automation_daily`, `kpi_automation_overall` | Доля диалогов, закрытых ботом без эскалации, против escalation rate. | `events` |
| `funnel.sql` | `kpi_funnel_events`, `kpi_funnel_entity_stage`, `kpi_funnel`, `kpi_funnel_steps` | Воронка лид → квалифицированный → сделка → выигрыш и конверсии между стадиями. | `events` |
| `response_volume.sql` | `kpi_response_volume_daily`, `kpi_response_volume_hourly`, `kpi_response_volume_by_role` | Объём сообщений бот / человек / клиент по дням, каналам и часам суток. | `messages`, `conversations`, `events` |

## Быстрый старт

```bash
# 1. Применить витрины
psql "$DATABASE_URL" -f analytics/sql/00_apply.sql

# 2. Залить демо-данные (можно запускать повторно — идемпотентно)
psql "$DATABASE_URL" -f analytics/seed/seed_events.sql

# 3. Подключить Metabase к Postgres и собрать карточки
#    см. dashboards/README.md
```

В окружении docker-compose вместо `$DATABASE_URL` используйте:

```bash
docker compose exec -T postgres \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f /sql/00_apply.sql
```

## Демо-данные

`seed/seed_events.sql` создаёт 70 диалогов за 14 дней (14 × 5), с сообщениями и
полным набором событий (`message_received`, `bot_answered`, `message_sent`,
`escalated`, `lead_created`, `deal_stage_changed`, `llm_call`). Все строки помечены
`external_id LIKE 'demo-%'` / `payload->>'demo' = 'true'` и удаляются в начале
скрипта — повторный запуск не плодит дубликаты.

## Допущения по схеме событий

- `deal_stage_changed.payload` содержит целевую стадию в `to_stage` (фолбэк — `stage`)
  со значениями `qualified` / `deal` / `won`.
- Воронка кумулятивна: сущность (диалог, иначе контакт), достигшая стадии N,
  засчитывается во все предыдущие стадии.
- Диалог автоматизирован, если есть `bot_answered` и нет `escalated`.
- В `messages` нет признака автора outbound-сообщения; бот/человек в
  `response_volume` определяются по времени относительно `escalated` диалога
  (до эскалации — бот, после — человек). Для точного разделения желательно
  добавить поле автора в `messages` (см. ниже).

## Предлагаемые правки общих файлов (НЕ внесены)

- `infra/db/init.sql`: добавить в `messages` колонку `sender TEXT` (`bot` | `agent` | `customer`)
  — это даст точный, не эвристический расчёт `response_volume` бот vs человек.
- `infra/db/init.sql`: индексы `messages(conversation_id, direction, ts)` и
  `conversations(created_at, channel)` ускорят витрины TTFR и lead capture на объёме.
- Зафиксировать в контракте §5 ключи `payload` для `deal_stage_changed`
  (`to_stage`/`stage`) и `lead_created` (`source`, `lead_id`), на которые опираются витрины.
