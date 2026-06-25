-- ============================================================================
-- seed_events.sql — генератор демо-данных для дашбордов E6
-- ----------------------------------------------------------------------------
-- Наполняет contacts / conversations / messages / events согласованными
-- тестовыми данными за последние ~14 дней, чтобы дашборды Metabase можно было
-- проверить без боевых данных.
--
-- ИДЕМПОТЕНТНОСТЬ:
--   Все демо-строки помечаются признаком external_id LIKE 'demo-%'
--   (и payload->>'demo' = 'true' в events). В начале скрипт удаляет ранее
--   созданные демо-строки, поэтому его можно запускать повторно.
--
-- Запуск:
--   psql "$DATABASE_URL" -f analytics/seed/seed_events.sql
--   -- или --
--   docker compose exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
--     < analytics/seed/seed_events.sql
--
-- Объём: 14 дней × 5 диалогов = 70 диалогов, у каждого 2..6 сообщений
-- и набор событий (message_received, bot_answered, message_sent, опц. escalated,
-- lead_created, deal_stage_changed[qualified/deal/won], llm_call).
-- ============================================================================

BEGIN;

-- ----------------------------------------------------------------------------
-- 1. Очистка прошлых демо-данных.
--    contacts ON DELETE CASCADE → conversations → messages удаляются каскадно.
--    events не имеют FK, поэтому чистим явно по маркеру.
-- ----------------------------------------------------------------------------
DELETE FROM events   WHERE contact_external_id LIKE 'demo-%' OR payload ->> 'demo' = 'true';
DELETE FROM contacts WHERE external_id LIKE 'demo-%';

-- ----------------------------------------------------------------------------
-- 2. Скелет демо-диалогов. Детерминированные значения (modulo) вместо random(),
--    чтобы повторные прогоны давали воспроизводимую картину.
-- ----------------------------------------------------------------------------
CREATE TEMP TABLE _demo ON COMMIT DROP AS
WITH base AS (
    SELECT d AS day_offset, n AS seq
    FROM generate_series(0, 13) AS d
    CROSS JOIN generate_series(1, 5) AS n
),
calc AS (
    SELECT
        day_offset,
        seq,
        -- псевдослучайные «корзины» 0..99 для распределения стадий и эскалаций
        ((seq * 31 + day_offset * 17) % 100) AS r_stage,
        ((seq * 13 + day_offset * 7)  % 100) AS r_esc
    FROM base
)
SELECT
    gen_random_uuid() AS conversation_id,
    gen_random_uuid() AS contact_id,
    'demo-' || to_char(CURRENT_DATE - day_offset, 'YYYYMMDD')
             || '-' || lpad(seq::text, 2, '0')                       AS external_id,
    (ARRAY['whatsapp', 'telegram', 'whatsapp', 'telegram', 'whatsapp'])[seq] AS channel,
    -- старт диалога в «рабочие часы» выбранного дня
    ((CURRENT_DATE - day_offset)::timestamptz
        + (( 8 + (seq * 2 + day_offset) % 10)        || ' hours')::interval
        + ((     (day_offset * 7 + seq * 13) % 60)   || ' minutes')::interval)  AS started_at,
    -- TTFR: ~каждый 5-й диалог отвечается медленно (формирует хвост p95)
    CASE WHEN (seq + day_offset) % 5 = 0
         THEN 600 + (day_offset * 37) % 900            -- 10..25 мин
         ELSE 5  + (seq * 7 + day_offset * 3) % 50     -- 5..55 сек
    END                                                              AS ttfr_seconds,
    2 + (seq + day_offset) % 5                                       AS msg_count,
    (r_stage < 70)                                                   AS is_lead,       -- ~70%
    (r_stage < 45)                                                   AS is_qualified,  -- ~45%
    (r_stage < 25)                                                   AS is_deal,       -- ~25%
    (r_stage < 12)                                                   AS is_won,        -- ~12%
    (r_esc   < 22)                                                   AS is_escalated   -- ~22%
FROM calc;

-- ----------------------------------------------------------------------------
-- 3. Контакты.
-- ----------------------------------------------------------------------------
INSERT INTO contacts (id, external_id, channel, phone, username, display_name, created_at)
SELECT
    contact_id,
    external_id,
    channel,
    '+99670' || lpad((abs(hashtext(external_id)) % 10000000)::text, 7, '0'),
    'user_' || external_id,
    'Demo ' || initcap(channel) || ' ' || external_id,
    started_at
FROM _demo;

-- ----------------------------------------------------------------------------
-- 4. Диалоги. Эскалированные диалоги получают assigned_to (человек-оператор).
-- ----------------------------------------------------------------------------
INSERT INTO conversations (id, contact_id, channel, status, assigned_to, last_message_at, created_at)
SELECT
    conversation_id,
    contact_id,
    channel,
    CASE WHEN is_escalated THEN 'escalated' ELSE 'closed' END,
    CASE WHEN is_escalated THEN 'agent-' || (1 + (abs(hashtext(external_id)) % 3))::text END,
    started_at + ((ttfr_seconds + msg_count * 180) || ' seconds')::interval,
    started_at
FROM _demo;

-- ----------------------------------------------------------------------------
-- 5. Сообщения. Нечётные = inbound (клиент), чётные = outbound (ответ).
--    i=1 (inbound) в started_at; i=2 (первый outbound) в started_at + ttfr.
-- ----------------------------------------------------------------------------
INSERT INTO messages (conversation_id, direction, content_type, text, ts)
SELECT
    d.conversation_id,
    CASE WHEN i % 2 = 1 THEN 'inbound' ELSE 'outbound' END,
    'text',
    CASE WHEN i % 2 = 1
         THEN 'Демо входящее сообщение #' || i
         ELSE 'Демо ответ #' || i END,
    d.started_at
      + CASE WHEN i = 1 THEN interval '0 seconds'
             ELSE (d.ttfr_seconds || ' seconds')::interval + ((i - 2) * interval '4 minutes')
        END
FROM _demo d
CROSS JOIN LATERAL generate_series(1, d.msg_count) AS i;

-- ----------------------------------------------------------------------------
-- 6. События. Каждый блок — отдельный тип события в едином формате §5.
--    Эскалация ставится через 120 сек после ответа бота, поэтому часть
--    последующих outbound-сообщений эскалированных диалогов попадёт в human.
-- ----------------------------------------------------------------------------
INSERT INTO events (event_type, conversation_id, contact_external_id, channel, payload, ts)
-- 6.1 message_received (первое входящее)
SELECT 'message_received', conversation_id, external_id, channel,
       jsonb_build_object('demo', true),
       started_at
FROM _demo
UNION ALL
-- 6.2 bot_answered (бот ответил)
SELECT 'bot_answered', conversation_id, external_id, channel,
       jsonb_build_object('demo', true, 'intent', 'faq', 'latency_ms', ttfr_seconds * 1000),
       started_at + (ttfr_seconds || ' seconds')::interval
FROM _demo
UNION ALL
-- 6.3 message_sent (исходящее бота)
SELECT 'message_sent', conversation_id, external_id, channel,
       jsonb_build_object('demo', true, 'sender', 'bot'),
       started_at + (ttfr_seconds || ' seconds')::interval
FROM _demo
UNION ALL
-- 6.4 escalated (передача человеку)
SELECT 'escalated', conversation_id, external_id, channel,
       jsonb_build_object('demo', true, 'reason', 'low_confidence'),
       started_at + ((ttfr_seconds + 120) || ' seconds')::interval
FROM _demo WHERE is_escalated
UNION ALL
-- 6.5 lead_created
SELECT 'lead_created', conversation_id, external_id, channel,
       jsonb_build_object('demo', true, 'source', channel, 'lead_id', 'L-' || external_id),
       started_at + interval '5 minutes'
FROM _demo WHERE is_lead
UNION ALL
-- 6.6 deal_stage_changed → qualified
SELECT 'deal_stage_changed', conversation_id, external_id, channel,
       jsonb_build_object('demo', true, 'to_stage', 'qualified', 'deal_id', 'D-' || external_id),
       started_at + interval '2 hours'
FROM _demo WHERE is_qualified
UNION ALL
-- 6.7 deal_stage_changed → deal
SELECT 'deal_stage_changed', conversation_id, external_id, channel,
       jsonb_build_object('demo', true, 'to_stage', 'deal', 'deal_id', 'D-' || external_id),
       started_at + interval '20 hours'
FROM _demo WHERE is_deal
UNION ALL
-- 6.8 deal_stage_changed → won
SELECT 'deal_stage_changed', conversation_id, external_id, channel,
       jsonb_build_object('demo', true, 'to_stage', 'won', 'deal_id', 'D-' || external_id),
       started_at + interval '36 hours'
FROM _demo WHERE is_won
UNION ALL
-- 6.9 llm_call (нагрузка LLM-шлюза)
SELECT 'llm_call', conversation_id, external_id, channel,
       jsonb_build_object('demo', true, 'model', 'gpt-4o-mini', 'tier', 'cheap',
                          'tokens', 200 + msg_count * 120),
       started_at + (ttfr_seconds || ' seconds')::interval
FROM _demo;

COMMIT;

-- Быстрая проверка наполнения (раскомментируйте при ручном запуске):
-- SELECT 'contacts'      AS t, count(*) FROM contacts      WHERE external_id LIKE 'demo-%'
-- UNION ALL SELECT 'conversations', count(*) FROM conversations WHERE assigned_to IS NOT NULL OR status='closed'
-- UNION ALL SELECT 'events', count(*) FROM events WHERE payload->>'demo' = 'true';
