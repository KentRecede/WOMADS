-- Stand-in for Supabase's auth.users table, ONLY for a plain local Postgres
-- (for example the Docker one in README.md) used to run the integration tests.
-- Never run this on a real Supabase project: it already has a real auth schema.
create schema if not exists auth;
create table if not exists auth.users (
    id                  uuid primary key default gen_random_uuid(),
    email               text,
    raw_app_meta_data   jsonb default '{}'::jsonb,
    raw_user_meta_data  jsonb default '{}'::jsonb
);
