# Дашборды Metabase (E6)

Дашборды строятся поверх KPI-витрин из [`../sql/`](../sql/). Все витрины имеют
префикс `kpi_` и пересоздаются командой `CREATE OR REPLACE VIEW`, поэтому
безопасны для повторного применения.

---

## 1. Подключение Metabase к PostgreSQL

Metabase и PostgreSQL поднимаются в общем `docker-compose.yml` (Metabase на
порту `3000`, в одной docker-сети `kivano` с контейнером `postgres`).

1. Запустите стек: `cp .env.example .env && docker compose up --build` (из корня репозитория).
2. Откройте `http://localhost:3000`, пройдите первичную настройку администратора.
3. **Admin settings → Databases → Add database → PostgreSQL**:
   - **Host:** `postgres`  (имя сервиса в docker-сети, НЕ `localhost`)
   - **Port:** `5432`
   - **Database name:** значение `POSTGRES_DB` (по умолчанию `kivano`)
   - **Username / Password:** `POSTGRES_USER` / `POSTGRES_PASSWORD` из `.env`
4. Сохраните. Metabase просканирует схему и подхватит таблицы и витрины `kpi_*`.

> Если Metabase запущен вне docker-compose, используйте host машины и проброшенный
> порт `5432` вместо имени `postgres`.

## 2. Применение витрин и демо-данных

Витрины (из окружения docker-compose):

```bash
# скопировать каталог sql в контейнер либо смонтировать том, затем:
docker compose exec -T postgres \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f /sql/00_apply.sql
```

или локально, при наличии `psql` и `DATABASE_URL`:

```bash
psql "$DATABASE_URL" -f analytics/sql/00_apply.sql      # витрины
psql "$DATABASE_URL" -f analytics/seed/seed_events.sql  # демо-данные (идемпотентно)
```

После загрузки демо-данных в Metabase выполните **Sync database schema now**
(Admin → Databases → ваша БД), чтобы появились новые витрины.

---

## 3. Дашборд «KPI продаж Kivano» — карточки

Каждая карточка — это либо GUI-вопрос к витрине `kpi_*`, либо нативный
SQL-запрос (ниже приведены готовые SELECT'ы для режима *Native query*).

### 3.1 TTFR — время первого ответа
| Карточка | Витрина | Тип визуализации |
|---|---|---|
| TTFR — медиана (тренд по дням) | `kpi_ttfr_daily_total` | Line: X=`day`, Y=`median_ttfr_seconds` |
| TTFR — p95 (тренд по дням) | `kpi_ttfr_daily_total` | Line: X=`day`, Y=`p95_ttfr_seconds` |
| TTFR по каналам | `kpi_ttfr_daily` | Line/Bar, серии по `channel` |
| TTFR — детализация по диалогам | `kpi_ttfr_per_conversation` | Table (drill-down) |

```sql
-- TTFR медиана и p95 по дням
SELECT day, median_ttfr_seconds, p95_ttfr_seconds
FROM kpi_ttfr_daily_total
ORDER BY day;
```

### 3.2 Lead capture rate
| Карточка | Витрина | Тип |
|---|---|---|
| Lead capture rate по дням | `kpi_lead_capture_daily` | Line: X=`day`, Y=`lead_capture_rate` |
| Лиды по каналам | `kpi_lead_capture_by_channel` | Bar: X=`channel`, Y=`leads_created` |
| Входящие диалоги vs лиды | `kpi_lead_capture_daily` | Combo: `inbound_conversations`, `leads_created` |

```sql
SELECT day, channel, inbound_conversations, leads_created, lead_capture_rate
FROM kpi_lead_capture_daily
ORDER BY day, channel;
```

### 3.3 Automation vs Escalation
| Карточка | Витрина | Тип |
|---|---|---|
| Automation rate (число) | `kpi_automation_overall` | Number/Gauge: `automation_rate` |
| Escalation rate (число) | `kpi_automation_overall` | Number/Gauge: `escalation_rate` |
| Automation vs Escalation по дням | `kpi_automation_daily` | Line: `automation_rate`, `escalation_rate` |

```sql
SELECT day, channel, automation_rate, escalation_rate,
       automated_conversations, escalated_conversations, conversations
FROM kpi_automation_daily
ORDER BY day, channel;
```

### 3.4 Воронка продаж
| Карточка | Витрина | Тип |
|---|---|---|
| Воронка лид→квалификация→сделка→выигрыш | `kpi_funnel_steps` | Funnel: dimension=`stage`, metric=`value` (сортировка по `step_order`) |
| Конверсии по каналам | `kpi_funnel` | Table |

```sql
-- Для Funnel-визуализации (длинный формат)
SELECT stage, value FROM kpi_funnel_steps ORDER BY step_order;
```

### 3.5 Объём сообщений: бот vs человек
| Карточка | Витрина | Тип |
|---|---|---|
| Сообщения бот/человек/клиент по дням | `kpi_response_volume_by_role` | Stacked bar: X=`day`, Y=`messages`, серии=`role` |
| Нагрузка по часам суток | `kpi_response_volume_hourly` | Bar: X=`hour_of_day` |
| Объём по каналам | `kpi_response_volume_daily` | Bar/Table |

```sql
SELECT day, role, sum(messages) AS messages
FROM kpi_response_volume_by_role
GROUP BY day, role
ORDER BY day, role;
```

---

## 4. Экспорт / воссоздание дашбордов

Карточки выше воссоздаются вручную по таблице за несколько минут. Для
версионирования определений дашбордов между средами используйте штатную
сериализацию Metabase:

```bash
# Экспорт (внутри контейнера metabase), результат — YAML-файлы коллекций/вопросов
docker compose exec metabase \
  java -jar /app/metabase.jar export /metabase-data/export

# Импорт в другую инсталляцию
docker compose exec metabase \
  java -jar /app/metabase.jar import /metabase-data/export
```

Скопируйте полученный каталог `export/` в `analytics/dashboards/` для хранения
в репозитории. Сериализация привязана к версии Metabase и к точным именам
витрин `kpi_*` — поэтому сначала применяйте `sql/00_apply.sql`, затем импорт.

> Пока в репозитории хранятся **определения витрин + спецификация карточек**
> (этот файл). Бинарный/YAML-экспорт добавляется по мере стабилизации дашбордов.
