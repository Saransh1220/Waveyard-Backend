DROP INDEX IF EXISTS idx_user_sessions_browser_session_id;

ALTER TABLE user_sessions
    DROP COLUMN IF EXISTS browser_session_id;

DROP TABLE IF EXISTS browser_sessions;
