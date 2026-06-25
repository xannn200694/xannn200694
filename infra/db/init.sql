-- Схема БД платформы (контракт §6). Применяется при первом старте PostgreSQL.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS contacts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id     TEXT,
    channel         TEXT,
    phone           TEXT,
    username        TEXT,
    display_name    TEXT,
    crm_contact_id  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (channel, external_id)
);

CREATE TABLE IF NOT EXISTS conversations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contact_id      UUID REFERENCES contacts(id) ON DELETE CASCADE,
    channel         TEXT,
    status          TEXT DEFAULT 'open',          -- open | bot | escalated | closed
    assigned_to     TEXT,
    last_message_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS messages (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id     UUID REFERENCES conversations(id) ON DELETE CASCADE,
    direction           TEXT NOT NULL,            -- inbound | outbound
    content_type        TEXT DEFAULT 'text',
    text                TEXT,
    provider_message_id TEXT,
    ts                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS events (
    event_id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type          TEXT NOT NULL,
    conversation_id     UUID,
    contact_external_id TEXT,
    channel             TEXT,
    payload             JSONB DEFAULT '{}'::jsonb,
    ts                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS prompts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    version         TEXT NOT NULL DEFAULT 'v1',
    body            TEXT NOT NULL,
    model_defaults  JSONB DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name, version)
);

CREATE INDEX IF NOT EXISTS idx_events_type_ts ON events (event_type, ts);
CREATE INDEX IF NOT EXISTS idx_events_conversation ON events (conversation_id);
CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages (conversation_id);
CREATE INDEX IF NOT EXISTS idx_contacts_phone ON contacts (phone);

-- Базовый системный промпт для ассистента отдела продаж (черновик, калибруется в E1).
INSERT INTO prompts (name, version, body, model_defaults)
VALUES (
    'sales_assistant',
    'v1',
    'Ты — вежливый ассистент отдела продаж компании Kivano. Отвечай кратко и по делу на языке клиента. ' ||
    'Используй ТОЛЬКО предоставленный контекст из базы знаний. Если ответа нет в контексте — честно скажи об этом ' ||
    'и предложи передать диалог менеджеру. Не выдумывай цены, сроки и обещания.',
    '{"temperature": 0.2, "tier": "cheap"}'::jsonb
)
ON CONFLICT (name, version) DO NOTHING;
