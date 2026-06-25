-- ============================================================================
-- Automation Rate vs Escalation Rate
-- ----------------------------------------------------------------------------
-- Источник: таблица events.
-- Считаем долю диалогов, закрытых ботом без эскалации (есть bot_answered и нет
-- последующего escalated), против доли эскалированных диалогов.
--
-- Допущения:
--  * Диалог считается автоматизированным, если по нему был хотя бы один
--    bot_answered и НЕ было события escalated. (escalated трактуется как
--    "после бота потребовался человек".)
--  * Диалог считается эскалированным, если есть хотя бы одно событие escalated.
--  * День метрики берём по времени первого значимого события диалога
--    (bot_answered либо escalated).
-- ============================================================================

-- Состояние автоматизации по каждому диалогу.
CREATE OR REPLACE VIEW kpi_conversation_automation AS
WITH per_conv AS (
    SELECT
        conversation_id,
        COALESCE(max(channel), 'unknown')                       AS channel,
        min(ts) FILTER (WHERE event_type = 'bot_answered')      AS first_bot_answer_at,
        min(ts) FILTER (WHERE event_type = 'escalated')         AS first_escalation_at,
        bool_or(event_type = 'bot_answered')                    AS has_bot_answer,
        bool_or(event_type = 'escalated')                       AS has_escalation
    FROM events
    WHERE conversation_id IS NOT NULL
    GROUP BY conversation_id
)
SELECT
    conversation_id,
    channel,
    first_bot_answer_at,
    first_escalation_at,
    has_bot_answer,
    has_escalation,
    (has_bot_answer AND NOT has_escalation)                     AS automated
FROM per_conv;

-- Дневные доли автоматизации и эскалации по каналам.
CREATE OR REPLACE VIEW kpi_automation_daily AS
WITH base AS (
    SELECT
        date_trunc('day', COALESCE(first_bot_answer_at, first_escalation_at))::date AS day,
        channel,
        automated,
        has_escalation
    FROM kpi_conversation_automation
    WHERE COALESCE(first_bot_answer_at, first_escalation_at) IS NOT NULL
)
SELECT
    day,
    channel,
    count(*)                                                                    AS conversations,
    count(*) FILTER (WHERE automated)                                           AS automated_conversations,
    count(*) FILTER (WHERE has_escalation)                                      AS escalated_conversations,
    round(count(*) FILTER (WHERE automated)::numeric
          / nullif(count(*), 0), 4)                                            AS automation_rate,
    round(count(*) FILTER (WHERE has_escalation)::numeric
          / nullif(count(*), 0), 4)                                            AS escalation_rate
FROM base
GROUP BY 1, 2;

-- Итоговые доли за всё время (headline-карточки automation/escalation rate).
CREATE OR REPLACE VIEW kpi_automation_overall AS
SELECT
    count(*)                                                  AS conversations,
    count(*) FILTER (WHERE automated)                         AS automated_conversations,
    count(*) FILTER (WHERE has_escalation)                    AS escalated_conversations,
    round(count(*) FILTER (WHERE automated)::numeric
          / nullif(count(*), 0), 4)                          AS automation_rate,
    round(count(*) FILTER (WHERE has_escalation)::numeric
          / nullif(count(*), 0), 4)                          AS escalation_rate
FROM kpi_conversation_automation;
