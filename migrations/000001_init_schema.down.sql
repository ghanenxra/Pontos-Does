DROP INDEX IF EXISTS idx_watch_events_session_id;
DROP INDEX IF EXISTS idx_watch_sessions_updated_at;
DROP INDEX IF EXISTS idx_watch_sessions_user_video;
DROP INDEX IF EXISTS idx_videos_user_source;
DROP INDEX IF EXISTS idx_oauth_tokens_user_id;
DROP INDEX IF EXISTS idx_sessions_expires_at;
DROP INDEX IF EXISTS idx_sessions_token_hash;

DROP TABLE IF EXISTS watch_events;
DROP TABLE IF EXISTS watch_sessions;
DROP TABLE IF EXISTS videos;
DROP TABLE IF EXISTS oauth_tokens;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;

DROP TYPE IF EXISTS event_type_enum;
DROP TYPE IF EXISTS source_type_enum;
