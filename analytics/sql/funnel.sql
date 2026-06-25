-- ============================================================================
-- Sales Funnel — воронка: лид → квалифицированный → сделка → выигрыш
-- ----------------------------------------------------------------------------
-- Источник: таблица events.
--   * lead_created                       → стадия 'lead'
--   * deal_stage_changed (payload->stage) → стадии 'qualified' / 'deal' / 'won'
--
-- Допущения по payload событий deal_stage_changed:
--  * Целевая стадия читается из payload как to_stage, при отсутствии — stage.
--    Поддерживаемые значения (lower-case): 'qualified', 'deal', 'won'
--    (а также синонимы 'lost' игнорируется в положительной воронке).
--  * Сущность воронки = conversation_id, при его отсутствии — contact_external_id.
--  * Воронка кумулятивна: сущность, достигшая стадии N, засчитывается во все
--    предыдущие стадии (через максимальный достигнутый ранг стадии). Это даёт
--    корректные конверсии даже если для сущности пришло только событие 'won'.
-- ============================================================================

-- Нормализованные события стадий воронки.
CREATE OR REPLACE VIEW kpi_funnel_events AS
SELECT
    event_id,
    conversation_id,
    contact_external_id,
    COALESCE(channel, 'unknown') AS channel,
    ts,
    CASE
        WHEN event_type = 'lead_created' THEN 'lead'
        WHEN event_type = 'deal_stage_changed'
            THEN lower(COALESCE(payload ->> 'to_stage', payload ->> 'stage'))
    END AS stage
FROM events
WHERE event_type IN ('lead_created', 'deal_stage_changed');

-- Максимальная достигнутая стадия по каждой сущности (диалог/контакт).
CREATE OR REPLACE VIEW kpi_funnel_entity_stage AS
SELECT
    COALESCE(conversation_id::text, contact_external_id) AS entity,
    max(channel)                                         AS channel,
    max(CASE stage
            WHEN 'lead'      THEN 1
            WHEN 'qualified' THEN 2
            WHEN 'deal'      THEN 3
            WHEN 'won'       THEN 4
        END)                                             AS max_stage_rank
FROM kpi_funnel_events
WHERE stage IN ('lead', 'qualified', 'deal', 'won')
  AND COALESCE(conversation_id::text, contact_external_id) IS NOT NULL
GROUP BY 1;

-- Воронка по каналам + итог (ROLLUP даёт строку channel IS NULL = все каналы).
CREATE OR REPLACE VIEW kpi_funnel AS
SELECT
    channel,
    count(*) FILTER (WHERE max_stage_rank >= 1) AS leads,
    count(*) FILTER (WHERE max_stage_rank >= 2) AS qualified,
    count(*) FILTER (WHERE max_stage_rank >= 3) AS deals,
    count(*) FILTER (WHERE max_stage_rank >= 4) AS won,
    round(count(*) FILTER (WHERE max_stage_rank >= 2)::numeric
          / nullif(count(*) FILTER (WHERE max_stage_rank >= 1), 0), 4) AS lead_to_qualified_rate,
    round(count(*) FILTER (WHERE max_stage_rank >= 3)::numeric
          / nullif(count(*) FILTER (WHERE max_stage_rank >= 2), 0), 4) AS qualified_to_deal_rate,
    round(count(*) FILTER (WHERE max_stage_rank >= 4)::numeric
          / nullif(count(*) FILTER (WHERE max_stage_rank >= 3), 0), 4) AS deal_to_won_rate,
    round(count(*) FILTER (WHERE max_stage_rank >= 4)::numeric
          / nullif(count(*) FILTER (WHERE max_stage_rank >= 1), 0), 4) AS lead_to_won_rate
FROM kpi_funnel_entity_stage
GROUP BY ROLLUP (channel);

-- Длинный формат воронки (одна строка на стадию) — удобно для Funnel-визуализации
-- Metabase, которая ожидает пары "стадия / значение".
CREATE OR REPLACE VIEW kpi_funnel_steps AS
WITH totals AS (
    SELECT
        count(*) FILTER (WHERE max_stage_rank >= 1) AS leads,
        count(*) FILTER (WHERE max_stage_rank >= 2) AS qualified,
        count(*) FILTER (WHERE max_stage_rank >= 3) AS deals,
        count(*) FILTER (WHERE max_stage_rank >= 4) AS won
    FROM kpi_funnel_entity_stage
)
SELECT step_order, stage, value
FROM totals,
LATERAL (VALUES
    (1, 'Лид',                leads),
    (2, 'Квалифицированный',  qualified),
    (3, 'Сделка',             deals),
    (4, 'Выигрыш',            won)
) AS s(step_order, stage, value);
