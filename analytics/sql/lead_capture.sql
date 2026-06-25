-- ============================================================================
-- Lead Capture — захват лидов
-- ----------------------------------------------------------------------------
-- Источники:
--   * conversations / contacts — входящие диалоги и уникальные контакты по дням/каналам.
--   * events (event_type = 'lead_created') — созданные лиды по дням/каналам.
--
-- Метрики:
--   * inbound_conversations — число входящих диалогов;
--   * unique_contacts       — число уникальных входящих контактов;
--   * leads_created         — число событий lead_created;
--   * lead_capture_rate     = leads_created / unique_contacts.
--
-- Допущения:
--  * "Входящий диалог" = любая строка conversations (диалог заводится на входящее
--    обращение). День берём по conversations.created_at.
--  * Канал лида берём из events.channel; контактов — из conversations.channel.
--  * lead_capture_rate теоретически может быть > 1, если на один контакт создано
--    несколько лидов — это сознательно не ограничивается.
-- ============================================================================

CREATE OR REPLACE VIEW kpi_lead_capture_daily AS
WITH conv AS (
    SELECT
        created_at::date              AS day,
        COALESCE(channel, 'unknown') AS channel,
        count(*)                     AS inbound_conversations,
        count(DISTINCT contact_id)   AS unique_contacts
    FROM conversations
    GROUP BY 1, 2
),
leads AS (
    SELECT
        ts::date                     AS day,
        COALESCE(channel, 'unknown') AS channel,
        count(*)                     AS leads_created
    FROM events
    WHERE event_type = 'lead_created'
    GROUP BY 1, 2
)
SELECT
    COALESCE(conv.day, leads.day)                          AS day,
    COALESCE(conv.channel, leads.channel)                  AS channel,
    COALESCE(conv.inbound_conversations, 0)                AS inbound_conversations,
    COALESCE(conv.unique_contacts, 0)                      AS unique_contacts,
    COALESCE(leads.leads_created, 0)                       AS leads_created,
    CASE
        WHEN COALESCE(conv.unique_contacts, 0) > 0
        THEN round(COALESCE(leads.leads_created, 0)::numeric / conv.unique_contacts, 4)
        ELSE NULL
    END                                                    AS lead_capture_rate
FROM conv
FULL OUTER JOIN leads
    ON conv.day = leads.day
   AND conv.channel = leads.channel;

-- Сводка по каналам за всё время (для bar-карточки "лиды по каналам").
CREATE OR REPLACE VIEW kpi_lead_capture_by_channel AS
SELECT
    channel,
    sum(inbound_conversations) AS inbound_conversations,
    sum(unique_contacts)       AS unique_contacts,
    sum(leads_created)         AS leads_created,
    CASE
        WHEN sum(unique_contacts) > 0
        THEN round(sum(leads_created)::numeric / sum(unique_contacts), 4)
        ELSE NULL
    END                        AS lead_capture_rate
FROM kpi_lead_capture_daily
GROUP BY channel;
