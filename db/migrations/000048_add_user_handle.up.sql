-- Public, unique, URL-safe handle for every account (used for /@handle profile URLs).
-- Derived from the artist (display) name, not the login name.

ALTER TABLE users ADD COLUMN handle VARCHAR(30);

CREATE OR REPLACE FUNCTION slugify_handle(src text) RETURNS text AS $$
    SELECT left(trim(both '-' from regexp_replace(lower(coalesce(src, '')), '[^a-z0-9]+', '-', 'g')), 30)
$$ LANGUAGE sql IMMUTABLE;

-- Returns the first free handle: base, base-2, base-3, ...
CREATE OR REPLACE FUNCTION next_free_handle(base text) RETURNS text AS $$
DECLARE
    root text := base;
    candidate text;
    n int := 1;
BEGIN
    IF length(root) < 3 THEN
        root := 'artist';
    END IF;
    candidate := root;
    WHILE EXISTS (SELECT 1 FROM users WHERE lower(handle) = candidate) LOOP
        n := n + 1;
        candidate := left(root, 30 - length(n::text) - 1) || '-' || n::text;
    END LOOP;
    RETURN candidate;
END;
$$ LANGUAGE plpgsql;

-- Backfill oldest accounts first so they get the cleanest handles.
DO $$
DECLARE
    r record;
BEGIN
    FOR r IN SELECT id, display_name, name FROM users WHERE handle IS NULL ORDER BY created_at, id LOOP
        UPDATE users
           SET handle = next_free_handle(slugify_handle(coalesce(nullif(r.display_name, ''), r.name)))
         WHERE id = r.id;
    END LOOP;
END $$;

ALTER TABLE users ALTER COLUMN handle SET NOT NULL;
CREATE UNIQUE INDEX users_handle_lower_uidx ON users (lower(handle));
ALTER TABLE users ADD CONSTRAINT users_handle_format_chk CHECK (handle ~ '^[a-z0-9][a-z0-9_-]{1,28}[a-z0-9]$');

-- Any future insert (email, Google, tests, scripts) gets a handle automatically.
CREATE OR REPLACE FUNCTION assign_user_handle() RETURNS trigger AS $$
BEGIN
    IF NEW.handle IS NULL OR NEW.handle = '' THEN
        NEW.handle := next_free_handle(slugify_handle(coalesce(nullif(NEW.display_name, ''), NEW.name)));
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_assign_handle
    BEFORE INSERT ON users
    FOR EACH ROW EXECUTE FUNCTION assign_user_handle();
