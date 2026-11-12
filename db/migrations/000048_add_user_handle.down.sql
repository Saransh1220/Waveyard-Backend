DROP TRIGGER IF EXISTS users_assign_handle ON users;
DROP FUNCTION IF EXISTS assign_user_handle();
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_handle_format_chk;
DROP INDEX IF EXISTS users_handle_lower_uidx;
ALTER TABLE users DROP COLUMN IF EXISTS handle;
DROP FUNCTION IF EXISTS next_free_handle(text);
DROP FUNCTION IF EXISTS slugify_handle(text);
