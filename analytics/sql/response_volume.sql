-- ============================================================================
-- Response Volume — объём сообщений: бот vs человек
-- ----------------------------------------------------------------------------
-- Источники: messages (+ conversations для канала), events (escalated — момент
-- передачи диалога человеку).
--
-- Классификация исходящих сообщений:
--  * customer_messages — все inbound (от клиента);
--  * bot_messages      — outbound ДО первой эскалации диалога (или если эскалации
--                        не было вовсе);
--  * human_messages    — outbound В МОМЕНТ/ПОСЛЕ первой эскалации диалога.
--
-- Допущения:
--  * В таблице messages нет признака отправителя outbound-сообщения, поэтому
--    бот/человек определяется по времени относительно события escalated данного
--    диалога. Это эвристика: после эскалации диалог ведёт человек-оператор.
--  * Если для метрики важна точность, рекомендуется добавить в messages поле
--    sender/author ('bot' | 'agent') — см. отчёт E6 (правки init.sql).
-- ============================================================================

-- Объём по дням и каналам.
CREATE OR REPLACE VIEW kpi_response_volume_daily AS
WITH esc AS (
    SELECT conversation_id, min(ts) AS escalated_at
    FROM events
    WHERE event_type = 'escalated'
      AND conversation_id IS NOT NULL
    GROUP BY conversation_id
)
SELECT
    m.ts::date                                          AS day,
    COALESCE(c.channel, 'unknown')                      AS channel,
    count(*) FILTER (WHERE m.direction = 'inbound')     AS customer_messages,
    count(*) FILTER (
        WHERE m.direction = 'outbound'
          AND (e.escalated_at IS NULL OR m.ts < e.escalated_at)
    )                                                   AS bot_messages,
    count(*) FILTER (
        WHERE m.direction = 'outbound'
          AND e.escalated_at IS NOT NULL
          AND m.ts >= e.escalated_at
    )                                                   AS human_messages,
    count(*)                                            AS total_messages
FROM messages m
LEFT JOIN conversations c ON c.id = m.conversation_id
LEFT JOIN esc e          ON e.conversation_id = m.conversation_id
GROUP BY 1, 2;

-- Профиль нагрузки по часам суток (heatmap "когда пишут").
CREATE OR REPLACE VIEW kpi_response_volume_hourly AS
WITH esc AS (
    SELECT conversation_id, min(ts) AS escalated_at
    FROM events
    WHERE event_type = 'escalated'
      AND conversation_id IS NOT NULL
    GROUP BY conversation_id
)
SELECT
    EXTRACT(HOUR FROM m.ts)::int                        AS hour_of_day,
    count(*) FILTER (WHERE m.direction = 'inbound')     AS customer_messages,
    count(*) FILTER (
        WHERE m.direction = 'outbound'
          AND (e.escalated_at IS NULL OR m.ts < e.escalated_at)
    )                                                   AS bot_messages,
    count(*) FILTER (
        WHERE m.direction = 'outbound'
          AND e.escalated_at IS NOT NULL
          AND m.ts >= e.escalated_at
    )                                                   AS human_messages,
    count(*)                                            AS total_messages
FROM messages m
LEFT JOIN esc e ON e.conversation_id = m.conversation_id
GROUP BY 1;

-- Длинный формат (день / роль / count) — удобно для stacked bar в Metabase.
CREATE OR REPLACE VIEW kpi_response_volume_by_role AS
SELECT day, channel, 'customer' AS role, customer_messages AS messages
FROM kpi_response_volume_daily
UNION ALL
SELECT day, channel, 'bot'      AS role, bot_messages      AS messages
FROM kpi_response_volume_daily
UNION ALL
SELECT day, channel, 'human'    AS role, human_messages    AS messages
FROM kpi_response_volume_daily;
