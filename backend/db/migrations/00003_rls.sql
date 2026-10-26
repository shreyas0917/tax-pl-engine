-- +goose Up
-- +goose StatementBegin
-- Row-level security (spec Section 7/13). The application connects with a login role and
-- immediately switches to the non-login role taxpl_app, which is subject to these policies.
-- Every user-scoped transaction sets `app.user_id`; a connection without it sees nothing.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'taxpl_app') THEN
        CREATE ROLE taxpl_app NOLOGIN;
    END IF;
END $$;

GRANT USAGE ON SCHEMA public, ref TO taxpl_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO taxpl_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ref TO taxpl_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO taxpl_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO taxpl_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO taxpl_app;

-- The audit log is append-only for the application role.
REVOKE UPDATE, DELETE, TRUNCATE ON audit_log FROM taxpl_app;

-- Let the migration/login role assume the application role.
DO $$ BEGIN EXECUTE format('GRANT taxpl_app TO %I', current_user); END $$;

-- System role for cross-user maintenance (raw-file purge). BYPASSRLS needs superuser to
-- create; when the migration role lacks it, the system pool falls back to the login role.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'taxpl_system') THEN
        BEGIN
            CREATE ROLE taxpl_system NOLOGIN BYPASSRLS;
        EXCEPTION WHEN insufficient_privilege THEN
            RAISE NOTICE 'cannot create taxpl_system (needs superuser); system pool will use the login role';
        END;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'taxpl_system') THEN
        EXECUTE 'GRANT USAGE ON SCHEMA public, ref TO taxpl_system';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO taxpl_system';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ref TO taxpl_system';
        EXECUTE 'GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO taxpl_system';
        EXECUTE 'ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO taxpl_system';
        EXECUTE 'ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO taxpl_system';
        EXECUTE 'REVOKE UPDATE, DELETE, TRUNCATE ON audit_log FROM taxpl_system';
        EXECUTE format('GRANT taxpl_system TO %I', current_user);
    END IF;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_user_id() RETURNS uuid
LANGUAGE sql STABLE AS $$
    SELECT NULLIF(current_setting('app.user_id', true), '')::uuid
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- users: anyone may look a user up (login) and register; only the user may change or delete.
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE users FORCE ROW LEVEL SECURITY;
CREATE POLICY users_select ON users FOR SELECT USING (true);
CREATE POLICY users_insert ON users FOR INSERT WITH CHECK (true);
CREATE POLICY users_update ON users FOR UPDATE USING (id = app_user_id());
CREATE POLICY users_delete ON users FOR DELETE USING (id = app_user_id());

-- refresh tokens are looked up by hash before a user is known.
ALTER TABLE refresh_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE refresh_tokens FORCE ROW LEVEL SECURITY;
CREATE POLICY refresh_all ON refresh_tokens USING (true) WITH CHECK (true);

-- Everything else is strictly per user.
ALTER TABLE demat_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE demat_accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON demat_accounts USING (user_id = app_user_id()) WITH CHECK (user_id = app_user_id());

ALTER TABLE uploads ENABLE ROW LEVEL SECURITY;
ALTER TABLE uploads FORCE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON uploads USING (user_id = app_user_id()) WITH CHECK (user_id = app_user_id());

ALTER TABLE jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON jobs USING (user_id = app_user_id()) WITH CHECK (user_id = app_user_id());

ALTER TABLE job_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_events FORCE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON job_events USING (user_id = app_user_id()) WITH CHECK (user_id = app_user_id());

ALTER TABLE trades ENABLE ROW LEVEL SECURITY;
ALTER TABLE trades FORCE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON trades USING (user_id = app_user_id()) WITH CHECK (user_id = app_user_id());

ALTER TABLE opening_lots ENABLE ROW LEVEL SECURITY;
ALTER TABLE opening_lots FORCE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON opening_lots USING (user_id = app_user_id()) WITH CHECK (user_id = app_user_id());

ALTER TABLE user_instrument_overrides ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_instrument_overrides FORCE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON user_instrument_overrides USING (user_id = app_user_id()) WITH CHECK (user_id = app_user_id());

ALTER TABLE computations ENABLE ROW LEVEL SECURITY;
ALTER TABLE computations FORCE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON computations USING (user_id = app_user_id()) WITH CHECK (user_id = app_user_id());

ALTER TABLE lots ENABLE ROW LEVEL SECURITY;
ALTER TABLE lots FORCE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON lots USING (user_id = app_user_id()) WITH CHECK (user_id = app_user_id());

ALTER TABLE gains ENABLE ROW LEVEL SECURITY;
ALTER TABLE gains FORCE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON gains USING (user_id = app_user_id()) WITH CHECK (user_id = app_user_id());

ALTER TABLE review_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE review_items FORCE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON review_items USING (user_id = app_user_id()) WITH CHECK (user_id = app_user_id());

ALTER TABLE reports ENABLE ROW LEVEL SECURITY;
ALTER TABLE reports FORCE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON reports USING (user_id = app_user_id()) WITH CHECK (user_id = app_user_id());

ALTER TABLE idempotency_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON idempotency_keys USING (user_id = app_user_id()) WITH CHECK (user_id = app_user_id());

-- audit_log: a user may read their own entries; inserts are allowed for any user_id
-- (registration and system jobs write rows before or without a user context).
ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log FORCE ROW LEVEL SECURITY;
CREATE POLICY audit_select ON audit_log FOR SELECT USING (user_id = app_user_id());
CREATE POLICY audit_insert ON audit_log FOR INSERT WITH CHECK (true);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
DECLARE t text;
BEGIN
    FOR t IN SELECT unnest(ARRAY['users','refresh_tokens','demat_accounts','uploads','jobs','job_events','trades','opening_lots',
        'user_instrument_overrides','computations','lots','gains','review_items','reports','idempotency_keys','audit_log']) LOOP
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
    END LOOP;
END $$;
DROP POLICY IF EXISTS users_select ON users; DROP POLICY IF EXISTS users_insert ON users;
DROP POLICY IF EXISTS users_update ON users; DROP POLICY IF EXISTS users_delete ON users;
DROP POLICY IF EXISTS refresh_all ON refresh_tokens;
DROP POLICY IF EXISTS own_rows ON demat_accounts; DROP POLICY IF EXISTS own_rows ON uploads; DROP POLICY IF EXISTS own_rows ON jobs;
DROP POLICY IF EXISTS own_rows ON job_events; DROP POLICY IF EXISTS own_rows ON trades; DROP POLICY IF EXISTS own_rows ON opening_lots;
DROP POLICY IF EXISTS own_rows ON user_instrument_overrides; DROP POLICY IF EXISTS own_rows ON computations; DROP POLICY IF EXISTS own_rows ON lots;
DROP POLICY IF EXISTS own_rows ON gains; DROP POLICY IF EXISTS own_rows ON review_items; DROP POLICY IF EXISTS own_rows ON reports;
DROP POLICY IF EXISTS own_rows ON idempotency_keys; DROP POLICY IF EXISTS audit_select ON audit_log; DROP POLICY IF EXISTS audit_insert ON audit_log;
DROP FUNCTION IF EXISTS app_user_id();
-- +goose StatementEnd
