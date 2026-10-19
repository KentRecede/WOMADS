-- =====================================================================
-- WOMADS — database schema (PostgreSQL / Supabase)
-- Source of truth: the ERD in the capstone paper (Figure 12), plus the
-- additions listed in README.md ("Where this differs from the ERD").
--
-- Safe to re-run: every statement is idempotent.
-- Run it in the Supabase SQL Editor, or:  psql "$DATABASE_URL" -f db/schema.sql
-- =====================================================================

-- ---------------------------------------------------------------------
-- USERS  (application profile; credentials live in Supabase auth.users)
-- The ERD has a `password` column here. We deliberately do NOT store
-- passwords: Supabase Auth owns them (bcrypt-hashed), which satisfies
-- the paper's security requirement 5.3.1 without us handling secrets.
-- ---------------------------------------------------------------------
create table if not exists public.users (
    user_id     uuid primary key references auth.users (id) on delete cascade,
    role        text not null default 'applicant'
                check (role in ('administrator','hr','payroll_officer','applicant','employee')),
    user_name   text not null,
    email       text not null,
    is_active   boolean not null default true,
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now(),
    deleted_at  timestamptz
);
create unique index if not exists users_email_key on public.users (lower(email)) where deleted_at is null;

-- ---------------------------------------------------------------------
-- CLIENTS
-- (ERD lists an employee_id column here; it is dropped because the
--  relationship is Employee -> Client, which employees.client_id and
--  deployments.client_id already express.)
-- ---------------------------------------------------------------------
create table if not exists public.clients (
    client_id          uuid primary key default gen_random_uuid(),
    company_name       text not null,
    contact_person     text not null,
    number_of_workers  integer not null check (number_of_workers > 0),
    start_date         date not null,
    end_date           date not null,
    created_at         timestamptz not null default now(),
    updated_at         timestamptz not null default now(),
    deleted_at         timestamptz,
    check (end_date >= start_date)
);

-- ---------------------------------------------------------------------
-- JOB OPENINGS  (status: draft -> active -> closed, per BPMN Figure 7)
-- ---------------------------------------------------------------------
create table if not exists public.job_openings (
    job_id           uuid primary key default gen_random_uuid(),
    client_id        uuid not null references public.clients (client_id),
    posted_by        uuid references public.users (user_id),
    job_title        text not null,
    job_description  text not null default '',
    location         text not null default '',
    category         text not null default '',
    status           text not null default 'draft' check (status in ('draft','active','closed')),
    created_at       timestamptz not null default now(),
    updated_at       timestamptz not null default now(),
    deleted_at       timestamptz
);
create index if not exists job_openings_status_idx on public.job_openings (status) where deleted_at is null;

-- ---------------------------------------------------------------------
-- APPLICANTS and the Personal Data Sheet sub-tables
-- (`age` from the ERD is omitted: derive it from birth_date.)
-- ---------------------------------------------------------------------
create table if not exists public.applicants (
    applicant_id     uuid primary key default gen_random_uuid(),
    user_id          uuid not null references public.users (user_id),
    job_id           uuid references public.job_openings (job_id),
    first_name       text not null,
    last_name        text not null,
    middle_name      text,
    nickname         text,
    address          text,
    sex              text,
    birth_date       date,
    birth_place      text,
    civil_status     text,
    religion         text,
    height           numeric(5,2),
    weight           numeric(5,2),
    citizenship      text,
    contact_no       text,
    email            text,
    status           text not null default 'pending',   -- pending | shortlisted | qualified | for_deployment | hired | rejected
    screening_score  numeric(6,3),
    created_at       timestamptz not null default now(),
    updated_at       timestamptz not null default now(),
    deleted_at       timestamptz
);
create index if not exists applicants_job_idx on public.applicants (job_id);

create table if not exists public.applicant_education (
    education_id    uuid primary key default gen_random_uuid(),
    applicant_id    uuid not null references public.applicants (applicant_id) on delete cascade,
    school_attended text not null,
    year_started    integer,
    year_ended      integer,
    degree_earned   text,
    units_earned    text,
    qualifications  text,
    certificates    text
);
create index if not exists applicant_education_applicant_idx on public.applicant_education (applicant_id);

create table if not exists public.applicant_work_history (
    history_id      uuid primary key default gen_random_uuid(),
    applicant_id    uuid not null references public.applicants (applicant_id) on delete cascade,
    company         text not null,
    position        text,
    reason_of_leave text
);
create index if not exists applicant_work_history_applicant_idx on public.applicant_work_history (applicant_id);

create table if not exists public.applicant_family (          -- strictly one-to-one
    family_id    uuid primary key default gen_random_uuid(),
    applicant_id uuid not null unique references public.applicants (applicant_id) on delete cascade,
    mother_name  text,
    father_name  text,
    age          integer,
    status       text,
    occupation   text,
    no_siblings  integer,
    rank         integer
);

create table if not exists public.applicant_residential (     -- one-to-one
    residential_id uuid primary key default gen_random_uuid(),
    applicant_id   uuid not null unique references public.applicants (applicant_id) on delete cascade,
    landmark       text,
    directions     text,
    location_sketch text      -- Supabase Storage object path, not the image itself
);

create table if not exists public.applicant_screening (       -- one-to-one
    screening_id       uuid primary key default gen_random_uuid(),
    applicant_id       uuid not null unique references public.applicants (applicant_id) on delete cascade,
    convicted          boolean,
    has_relatives      boolean,
    has_diseases       boolean,
    has_applied_before boolean
);

-- ---------------------------------------------------------------------
-- EMPLOYEES  (applicant_id is nullable: existing staff were never applicants here)
-- user_id (added) links an employee to a login for the Employee Dashboard.
-- ---------------------------------------------------------------------
create table if not exists public.employees (
    employee_id        uuid primary key default gen_random_uuid(),
    applicant_id       uuid unique references public.applicants (applicant_id),
    client_id          uuid references public.clients (client_id),     -- current assignment only
    user_id            uuid unique references public.users (user_id),
    name               text not null,
    position           text not null default '',
    employment_status  text not null default 'active'
                       check (employment_status in ('active','benched','on_leave','terminated')),
    created_at         timestamptz not null default now(),
    updated_at         timestamptz not null default now(),
    deleted_at         timestamptz
);
create index if not exists employees_client_idx on public.employees (client_id);

-- ---------------------------------------------------------------------
-- DEPLOYMENTS  (full placement history; never soft-deleted)
-- ---------------------------------------------------------------------
create table if not exists public.deployments (
    deployment_id  uuid primary key default gen_random_uuid(),
    employee_id    uuid not null references public.employees (employee_id),
    client_id      uuid not null references public.clients (client_id),
    start_date     date not null,
    end_date       date,
    status         text not null default 'active'
                   check (status in ('active','completed','reserved','cancelled')),
    created_at     timestamptz not null default now(),
    updated_at     timestamptz not null default now(),
    check (end_date is null or end_date >= start_date)
);
create index if not exists deployments_client_status_idx on public.deployments (client_id, status);
create index if not exists deployments_employee_idx on public.deployments (employee_id);

-- ---------------------------------------------------------------------
-- ATTENDANCE  (described in the ERD narrative; absent from the figure)
-- One row per deployment per day, imported from client Excel sheets and
-- verified by the payroll officer before payroll can run.
-- ---------------------------------------------------------------------
create table if not exists public.attendance (
    attendance_id  uuid primary key default gen_random_uuid(),
    deployment_id  uuid not null references public.deployments (deployment_id),
    work_date      date not null,
    hours_worked   numeric(5,2) not null default 0 check (hours_worked >= 0),
    overtime_hrs   numeric(5,2) not null default 0 check (overtime_hrs >= 0),
    night_hrs      numeric(5,2) not null default 0 check (night_hrs >= 0),
    late_minutes   integer not null default 0 check (late_minutes >= 0),
    verified       boolean not null default false,
    import_batch   text,
    unique (deployment_id, work_date)
);

-- ---------------------------------------------------------------------
-- PAYROLL  (ERD has hrs_worked, overtime_hrs, net_pay; the rest is needed
-- to produce payslips with SSS/PhilHealth/Pag-IBIG/tax per REQ-4.6-5)
-- ---------------------------------------------------------------------
create table if not exists public.payroll (
    payroll_id        uuid primary key default gen_random_uuid(),
    employee_id       uuid not null references public.employees (employee_id),
    period_start      date not null,
    period_end        date not null,
    hrs_worked        numeric(8,2) not null default 0,
    overtime_hrs      numeric(8,2) not null default 0,
    night_hrs         numeric(8,2) not null default 0,
    gross_pay         numeric(12,2) not null default 0,
    sss               numeric(12,2) not null default 0,
    philhealth        numeric(12,2) not null default 0,
    pagibig           numeric(12,2) not null default 0,
    withholding_tax   numeric(12,2) not null default 0,
    late_deduction    numeric(12,2) not null default 0,
    net_pay           numeric(12,2) not null default 0,
    status            text not null default 'draft' check (status in ('draft','verified','released')),
    created_at        timestamptz not null default now(),
    check (period_end >= period_start),
    unique (employee_id, period_start, period_end)
);

-- ---------------------------------------------------------------------
-- NOTIFICATIONS and AUDIT LOG (NFR 5.3.6: record critical admin actions)
-- ---------------------------------------------------------------------
create table if not exists public.notifications (
    notif_id    uuid primary key default gen_random_uuid(),
    user_id     uuid not null references public.users (user_id) on delete cascade,
    message     text not null,
    is_read     boolean not null default false,
    created_at  timestamptz not null default now()
);
create index if not exists notifications_user_idx on public.notifications (user_id, is_read);

create table if not exists public.audit_logs (
    audit_id    bigint generated always as identity primary key,
    actor_id    uuid,                 -- no FK: history must outlive the account
    action      text not null,
    entity      text not null,
    entity_id   text,
    ip          text,
    created_at  timestamptz not null default now()
);
create index if not exists audit_logs_created_idx on public.audit_logs (created_at desc);

-- ---------------------------------------------------------------------
-- AUTO-CREATE A PROFILE WHEN SOMEONE SIGNS UP IN SUPABASE AUTH
-- * Public sign-ups become 'applicant'.
-- * Staff accounts created by the backend carry the role in app_metadata,
--   which only the service key can write (user_metadata is user-editable
--   and must never be trusted for authorization).
-- ---------------------------------------------------------------------
create or replace function public.handle_new_auth_user()
returns trigger
language plpgsql
security definer
set search_path = ''
as $$
declare
    r text := coalesce(new.raw_app_meta_data ->> 'role', 'applicant');
begin
    if r not in ('administrator','hr','payroll_officer','applicant','employee') then
        r := 'applicant';
    end if;
    insert into public.users (user_id, role, user_name, email)
    values (
        new.id, r,
        coalesce(nullif(new.raw_user_meta_data ->> 'full_name', ''), split_part(new.email, '@', 1)),
        new.email
    )
    on conflict (user_id) do nothing;
    return new;
end;
$$;

drop trigger if exists on_auth_user_created on auth.users;
create trigger on_auth_user_created
    after insert on auth.users
    for each row execute function public.handle_new_auth_user();

-- ---------------------------------------------------------------------
-- ROW LEVEL SECURITY — deny by default.
-- Supabase auto-exposes public tables through its REST API using the
-- public anon key. With RLS on and NO policies, that door is closed for
-- every table; only our Go backend (which connects as the table owner)
-- can read or write. This is what keeps applicant PII private (RA 10173).
-- ---------------------------------------------------------------------
alter table public.users                 enable row level security;
alter table public.clients               enable row level security;
alter table public.job_openings          enable row level security;
alter table public.applicants            enable row level security;
alter table public.applicant_education   enable row level security;
alter table public.applicant_work_history enable row level security;
alter table public.applicant_family      enable row level security;
alter table public.applicant_residential enable row level security;
alter table public.applicant_screening   enable row level security;
alter table public.employees             enable row level security;
alter table public.deployments           enable row level security;
alter table public.attendance            enable row level security;
alter table public.payroll               enable row level security;
alter table public.notifications         enable row level security;
alter table public.audit_logs            enable row level security;
