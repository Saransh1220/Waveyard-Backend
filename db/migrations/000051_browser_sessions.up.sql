CREATE TABLE IF NOT EXISTS browser_sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    revoked_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_browser_sessions_user_id ON browser_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_browser_sessions_revoked_at ON browser_sessions(revoked_at);

ALTER TABLE user_sessions
    ADD COLUMN IF NOT EXISTS browser_session_id UUID NULL REFERENCES browser_sessions(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_user_sessions_browser_session_id ON user_sessions(browser_session_id);
