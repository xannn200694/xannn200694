-- ============================================================================
-- TTFR — Time To First Response
-- ----------------------------------------------------------------------------
-- Источник: таблица messages (+ conversations для канала).
-- Для каждого диалога считаем время между ПЕРВЫМ inbound и ПЕРВЫМ outbound
-- сообщением. Агрегаты по дням: медиана (p50) и p95 через percentile_cont.
--
-- Допущения:
--  * "Первый ответ" = первое исходящее сообщение после первого входящего.
--  * Диалоги без входящих или без исходящих в расчёт TTFR не попадают.
--  * Если первый outbound оказался раньше первого inbound (например, прокативный
--    шаблон) — диалог исключается, чтобы не получить отрицательный TTFR.
-- ============================================================================

-- TTFR по каждому диалогу (атомарная витрина для drill-down в Metabase).
CREATE OR REPLACE VIEW kpi_ttfr_per_conversation AS
WITH firsts AS (
    SELECT
        m.conversation_id,
        MIN(m.ts) FILTER (WHERE m.direction = 'inbound')  AS first_inbound_at,
        MIN(m.ts) FILTER (WHERE m.direction = 'outbound') AS first_outbound_at
    FROM messages m
    GROUP BY m.conversation_id
)
SELECT
    f.conversation_id,
    COALESCE(c.channel, 'unknown')                                  AS channel,
    f.first_inbound_at,
    f.first_outbound_at,
    EXTRACT(EPOCH FROM (f.first_outbound_at - f.first_inbound_at))  AS ttfr_seconds
FROM firsts f
LEFT JOIN conversations c ON c.id = f.conversation_id
WHERE f.first_inbound_at  IS NOT NULL
  AND f.first_outbound_at IS NOT NULL
  AND f.first_outbound_at >= f.first_inbound_at;

-- Агрегаты по дням и каналам: медиана и p95.
CREATE OR REPLACE VIEW kpi_ttfr_daily AS
SELECT
    date_trunc('day', first_inbound_at)::date                                AS day,
    channel,
    count(*)                                                                 AS conversations,
    round(avg(ttfr_seconds)::numeric, 1)                                     AS avg_ttfr_seconds,
    percentile_cont(0.5)  WITHIN GROUP (ORDER BY ttfr_seconds)               AS median_ttfr_seconds,
    percentile_cont(0.95) WITHIN GROUP (ORDER BY ttfr_seconds)               AS p95_ttfr_seconds
FROM kpi_ttfr_per_conversation
GROUP BY 1, 2;

-- Общий тренд по дням (без разбивки по каналам) — для headline-карточек.
CREATE OR REPLACE VIEW kpi_ttfr_daily_total AS
SELECT
    date_trunc('day', first_inbound_at)::date                                AS day,
    count(*)                                                                 AS conversations,
    percentile_cont(0.5)  WITHIN GROUP (ORDER BY ttfr_seconds)               AS median_ttfr_seconds,
    percentile_cont(0.95) WITHIN GROUP (ORDER BY ttfr_seconds)               AS p95_ttfr_seconds
FROM kpi_ttfr_per_conversation
GROUP BY 1;
