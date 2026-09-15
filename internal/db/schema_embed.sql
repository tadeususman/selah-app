-- JournalFlow initial schema (embedded copy — source of truth lives in
-- /migrations/0001_init.sql; keep the two in sync if you add tables).
-- Applied automatically on server startup (see internal/db/db.go).
-- Written to be idempotent so it's safe to run on every boot.

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    is_admin      BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS discuss_original_lang BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS latitude DOUBLE PRECISION;
ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS longitude DOUBLE PRECISION;

-- One row per devotion session ("Day N" in the reference UI).
CREATE TABLE IF NOT EXISTS journal_entries (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day_number       INT NOT NULL,
    entry_date       DATE NOT NULL DEFAULT CURRENT_DATE,
    entry_time       TIME NOT NULL DEFAULT CURRENT_TIME,
    location         TEXT NOT NULL DEFAULT '',

    verse_ref        TEXT NOT NULL DEFAULT '',   -- e.g. "Mazmur 23:1-3"
    verse_text       TEXT NOT NULL DEFAULT '',   -- pasted/typed verse
    ai_background    TEXT NOT NULL DEFAULT '',   -- AI: historical/original-language context
    reflection       TEXT NOT NULL DEFAULT '',   -- user's own reflection
    practical_step   TEXT NOT NULL DEFAULT '',   -- user's closing action step / summary

    status           TEXT NOT NULL DEFAULT 'draft'
                       CHECK (status IN ('draft','completed')),

    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(user_id, day_number)
);

CREATE INDEX IF NOT EXISTS idx_journal_entries_user_date
    ON journal_entries(user_id, entry_date DESC);

-- Full back-and-forth discussion with the AI for a given entry
-- (the "Ask AI: ada yang kurang jelas" loop in the flow diagram).
CREATE TABLE IF NOT EXISTS journal_messages (
    id           BIGSERIAL PRIMARY KEY,
    entry_id     BIGINT NOT NULL REFERENCES journal_entries(id) ON DELETE CASCADE,
    role         TEXT NOT NULL CHECK (role IN ('user','ai')),
    content      TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_journal_messages_entry
    ON journal_messages(entry_id, created_at ASC);

-- Simple server-side session store (cookie holds only the session id).
CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);

-- AI usage tracking for cost monitoring.
CREATE TABLE IF NOT EXISTS ai_usage_log (
    id            BIGSERIAL PRIMARY KEY,
    provider      TEXT NOT NULL,
    model         TEXT NOT NULL,
    user_id       BIGINT REFERENCES users(id) ON DELETE SET NULL,
    input_tokens  INT NOT NULL DEFAULT 0,
    output_tokens INT NOT NULL DEFAULT 0,
    cost_usd      NUMERIC(10,8) NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE ai_usage_log ADD COLUMN IF NOT EXISTS user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_ai_usage_log_user ON ai_usage_log(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_usage_log_date ON ai_usage_log(created_at DESC);

-- AI error tracking.
CREATE TABLE IF NOT EXISTS ai_error_log (
    id         BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    error_msg  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_error_log_date ON ai_error_log(created_at DESC);
