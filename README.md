# WOMADS Backend

Backend for **WOMADS: Web-Based Workforce Management and Deployment System for MJL Manpower Agency**
(Group 4 Capstone), built to the architecture in the paper: **Go** services behind an **API gateway**,
**GORM** on **Supabase PostgreSQL**, **Supabase Auth** (JWT), a **Python FastAPI** AI service, **Docker**,
**GitHub Actions** CI, and **Swagger/OpenAPI** docs.

This is an initialized foundation, not the finished system. Three modules work end to end
(Account Management, Client Management, Job Openings). The rest are already registered with the
correct role guards and answer `501 Not Implemented` until you build them. See [What's built](#whats-built).

---

## 1. How the pieces fit

```
 Browser (Next.js)
       │  HTTPS, "Authorization: Bearer <Supabase JWT>"
       ▼
 ┌──────────────┐   CORS, request ids, real client IP
 │   gateway    │   :8000   cmd/gateway
 └──────┬───────┘
        ▼
 ┌──────────────┐   verifies JWT → looks up role in `users` → role guard → handler
 │   core API   │   :8080   cmd/api            (Go + chi + GORM)
 └──┬───────┬───┘
    │       └───────────────► AI screening :8090  services/ai-screening (Python, later)
    ▼
 Supabase: PostgreSQL (data) + Auth (logins, JWTs) + Storage (resumes, IDs)
```

**One rule to remember:** Supabase Auth proves *who* someone is. Our own `users` table says *what they may
do* (their role). The role is never read from the token, because tokens contain user-editable fields.

## 2. Folder map

```
cmd/api/              starts the core API
cmd/gateway/          starts the gateway
db/schema.sql         the database (source of truth). Run it in Supabase.
db/dev/auth_stub.sql  fake auth table for local test databases only
internal/
  config/             reads .env / environment variables
  database/           opens the GORM connection
  models/             Go structs that mirror the tables
  httpx/              shared helpers: JSON replies, validation, paging, logging
  auth/               JWT verification, role lookup, role guard
  audit/              writes the audit log
  supabase/           creates login accounts through Supabase Auth
  modules/            ONE FOLDER PER BUSINESS MODULE  ← you will spend most time here
    accounts/ clients/ jobs/
  server/router.go    the whole API surface in one file (who may call what)
  server/stubs.go     routes for modules not built yet
  gateway/            the gateway
  docs/openapi.yaml   Swagger spec (hand-written; update it when endpoints change)
services/ai-screening/  Python placeholder for the AI service
```

---

## 3. Set it up (first time)

### Step 0: Install the tools

| Tool | Needed for | Check it |
|---|---|---|
| **Go 1.22 or newer** (go.dev/dl) | everything | `go version` |
| **Git** | version control | `git --version` |
| **VS Code** + the *Go* extension | editing | |
| **Docker Desktop** *(optional)* | `docker compose`, local test DB | `docker --version` |

### Step 1: Download the dependencies

From the project folder:

```bash
go mod tidy
```

This downloads the libraries and creates `go.sum`. **Commit `go.sum`** to Git. Docker and CI need it.

### Step 2: Create the Supabase project

1. Create a project at supabase.com. Save the **database password** somewhere safe.
2. In **Authentication**, make sure *Email* sign-in is on and *email confirmation* is on. Turn **off**
   anonymous and phone sign-ins; the schema assumes email accounts only.
3. In the Auth settings, set **JWT expiry** to `1800` seconds (30 min), which matches the paper's
   session requirement (5.3.5). A shorter token lifetime limits how long a stolen token is useful.
   Note: a true "log out after 30 minutes *idle*" also needs front-end logic or a Supabase setting that
   depends on your plan.

### Step 3: Collect four values from the Supabase dashboard

Dashboard menus get renamed from time to time; use the dashboard search if you can't find one.

| You need | Where | Goes in `.env` as |
|---|---|---|
| **Project URL** | Project Settings → API | `SUPABASE_URL` |
| **Database connection string** | the **Connect** button → *Session pooler* | `DATABASE_URL` |
| **Service-role / secret key** | Project Settings → API keys | `SUPABASE_SERVICE_ROLE_KEY` |
| **JWT secret** *(only if your project uses the legacy HS256 secret)* | Project Settings → JWT | `SUPABASE_JWT_SECRET` |

Two traps here:

* Use the **Session pooler** string. The "direct" connection is IPv6-only on Supabase, and many home and
  school networks have no IPv6. The code is set up to also tolerate the *transaction* pooler (port 6543), but I could only test against plain PostgreSQL.
* If your database password contains `@ : / # ? %`, **URL-encode it** in `DATABASE_URL` (`@` becomes `%40`).

Which JWT setup does your project have? You don't have to know. The API accepts both: legacy tokens
(`SUPABASE_JWT_SECRET`) and the newer asymmetric keys (fetched automatically from your project's JWKS
endpoint). If a login returns 401 and the API log says *"token is HS256 but SUPABASE_JWT_SECRET is not set"*,
fill in the secret.

### Step 4: Create `.env`

```bash
cp .env.example .env        # Windows PowerShell: Copy-Item .env.example .env
```

Fill in the values. `.env` is git-ignored; **never commit it**. The service-role key can create and delete
any user, so treat it like a root password.

### Step 5: Create the database tables

Open **SQL Editor** in Supabase, paste the whole of `db/schema.sql`, and press **Run**.
It is safe to run again later (every statement is idempotent). Check **Table Editor**: you should see
`users`, `clients`, `job_openings`, `applicants`, `employees`, `deployments`, `payroll`, and more.

What the script also does for you:
* turns on **Row Level Security with no policies** on every table, so Supabase's public API cannot read
  applicant data; only this backend can (important for RA 10173, the Data Privacy Act);
* installs a **trigger** that creates a `users` profile whenever someone signs up. Public sign-ups
  become `applicant`. Staff accounts created by the API get their role from `app_metadata`, which only
  the service key can write.

### Step 6: Make yourself the first administrator

1. Supabase → **Authentication → Users → Add user**. Enter your email and a strong password, and tick
   *auto-confirm*.
2. In the SQL Editor:
   ```sql
   update public.users set role = 'administrator' where email = 'you@example.com';
   ```
   (The trigger already created your profile as `applicant`; this promotes you.)

From now on, create other staff through the API (`POST /api/v1/users`), not in the dashboard.

### Step 7: Run it

Two terminals, in the project folder:

```bash
go run ./cmd/api        # terminal 1: core API on :8080
go run ./cmd/gateway    # terminal 2: gateway on :8000
```

Check that it's alive:

```bash
curl localhost:8000/healthz        # {"status":"ok"}
curl localhost:8000/readyz         # {"status":"ok"} means the database connection works
```

### Step 8: Sign in and call a protected endpoint

Get a token from Supabase using your **publishable / anon key** (the *public* one from the API keys page):

```bash
curl -s -X POST "$SUPABASE_URL/auth/v1/token?grant_type=password" \
  -H "apikey: YOUR_PUBLISHABLE_KEY" -H "Content-Type: application/json" \
  -d '{"email":"you@example.com","password":"YOUR_PASSWORD"}'
```

```powershell
# Windows PowerShell
Invoke-RestMethod -Method Post -Uri "$env:SUPABASE_URL/auth/v1/token?grant_type=password" `
  -Headers @{apikey="YOUR_PUBLISHABLE_KEY"} -ContentType "application/json" `
  -Body '{"email":"you@example.com","password":"YOUR_PASSWORD"}'
```

Copy the `access_token` from the reply, then:

```bash
curl -H "Authorization: Bearer <access_token>" localhost:8000/api/v1/me
```

You should see your email and `"role":"administrator"`.

### Step 9: Explore with Swagger

Open **http://localhost:8000/docs**, click **Authorize**, paste the `access_token`, and use *Try it out*.
(Docs are hidden when `APP_ENV=production`.)

### Step 10 (optional): Run everything in Docker

```bash
docker compose up --build                  # gateway + api, gateway on :8000
docker compose --profile ai up --build     # ...plus the Python AI placeholder
```

Docker needs `go.sum` (Step 1) and `.env` (Step 4). Inside Docker the gateway reaches the API at
`http://api:8080` automatically; the API is not published to your machine.

---

## 4. Run the tests

```bash
go test ./...
```

This runs unit tests (JWT checks, role guard, gateway, validation, config). The **integration tests**
(real router + real PostgreSQL) are skipped unless you give them a scratch database:

```bash
docker run --name womads-pg -e POSTGRES_PASSWORD=devpass -e POSTGRES_DB=womads_test -p 5433:5432 -d postgres:16
psql "postgresql://postgres:devpass@localhost:5433/womads_test" -v ON_ERROR_STOP=1 \
     -f db/dev/auth_stub.sql -f db/schema.sql
TEST_DATABASE_URL="postgresql://postgres:devpass@localhost:5433/womads_test?sslmode=disable" go test ./... -count=1
```

(PowerShell: set `$env:TEST_DATABASE_URL = "..."` first.) **Never point `TEST_DATABASE_URL` at your real
Supabase project;** the tests insert and delete rows. The tests clean up after themselves.

GitHub Actions (`.github/workflows/ci.yml`) runs the same thing on every push, with a Postgres service.

---

## 5. How a request flows (read this before writing code)

`GET /api/v1/clients` with a token:

1. **gateway**: checks CORS, stamps a request id, forwards to the API.
2. **router** (`internal/server/router.go`): matches the route, which sits inside a group with
   `Authenticate` and `RequireRoles(administrator, hr)`.
3. **Authenticate**: verifies the token signature, expiry, issuer and audience; loads the user from the
   `users` table; rejects unknown or deactivated accounts. (401 = who are you? 403 = not allowed.)
4. **RequireRoles**: is this role on the guest list for the route?
5. **handler** (`internal/modules/clients`): validates input, talks to the database through GORM, and
   answers in the standard JSON shape.

Every success is `{"data": ...}` (lists add `"meta"`); every failure is
`{"error": {"code", "message", "fields"?}}`.

### Add a module (recipe)

Example: Employee Management.

1. Make `internal/modules/employees/handler.go`. Copy `clients/handler.go` as your template: a
   `Handler{DB}`, a `Routes(r chi.Router)` method, an input struct with pointer fields, a `validate`
   function, then the handlers. The model already exists in `internal/models`.
2. In `internal/server/router.go`, create the handler and mount it inside the right role group:
   `r.Route("/employees", employeesH.Routes)`.
3. Delete the `/employees...` lines from the `planned` table in `internal/server/stubs.go`.
4. Add the endpoints to `internal/docs/openapi.yaml`.
5. Add an integration test in `internal/server/integration_test.go`, copying `TestClientsLifecycle`.

Call `audit.Record(...)` after every create, update or delete. The paper requires critical actions to be logged.

---

## 6. What's built

| Module (paper) | Status | Notes |
|---|---|---|
| Account Management (REQ-4.2) | ✅ working | admin-only; creates real Supabase logins; can't lock yourself out; archives instead of deleting |
| Client Management (REQ-4.4) | ✅ working | validation of contract fields, "View deployed employees" with quota, can't archive a client with active deployments |
| Job Openings (REQ-4.3-4..6) | ✅ working | draft → active → closed; public careers list hides the client |
| Online Application (REQ-4.1) | ⏳ routes + role guards only | next: applicant form, WSM ranking |
| Employee Management (REQ-4.3-1..3) | ⏳ routes + role guards only | |
| Employee Deployment (REQ-4.5) | ⏳ routes + role guards only | needs business rules 4 and 5 |
| Payroll (REQ-4.6) | ⏳ routes + role guards only | Excel import, SSS/PhilHealth/Pag-IBIG/tax, PDF payslips |
| Reports & Analytics (REQ-4.7) | ⏳ routes + role guards only | |
| Employee dashboard (REQ-4.8) | ⏳ routes + role guards only | `/me/...` endpoints |
| AI screening service | ⏳ placeholder | the paper schedules it last |

Suggested order: Employees → Applicants (+ ranking) → Deployments → Payroll → Reports → AI service.

## 7. Where this differs from the ERD (and why)

The ERD in the paper (Figure 12) is the starting point, but building from it surfaced gaps. Check these
with your adviser and update the paper's ERD to match.

| Change | Reason |
|---|---|
| `users.password` removed; `is_active` added | Supabase Auth stores passwords (bcrypt). We never handle them. |
| `clients.employee_id` removed | A client has many employees; `employees.client_id` and `deployments.client_id` already model it. |
| `job_openings.status` added (`draft/active/closed`) | Figure 7 describes drafts, publishing and closing, but the ERD has no column for it. |
| `job_openings.posted_by` added | The ERD narrative mentions it; the figure omits it. |
| `applicants.job_id`, `status`, `screening_score` added; `age` removed | The narrative says applicants link to a job and carry a score and status. Age should be computed from `birth_date`. |
| `applicant_residential.resident_id` renamed `residential_id`; `useru_id` → `user_id` | Typos in the figure. |
| `employees.user_id` added; `employees.applicant_id` nullable | Employees log in to the Employee Dashboard; existing staff were never applicants. |
| `deployments.client_id` and `end_date` added | The narrative says deployments reference the client; history needs an end. |
| `attendance` table added | The narrative describes it, but it's missing from the figure. Payroll cannot run without it (business rule 6). |
| `payroll`: period, gross, SSS, PhilHealth, Pag-IBIG, tax, late deduction, status added | REQ-4.6-5 and payslips need the breakdown, not just `net_pay`. |
| `notifications.is_read`, `audit_logs` added | Notifications need a read flag; NFR 5.3.6 requires logging critical actions. |
| Statuses and a few values are my assumptions | Employment status values come from the paper; applicant and deployment statuses are my reading of it. |

**Not in the schema yet:** coordinates for clients/applicants. The abstract says deployment matching
considers *proximity*, but nothing in the ERD can store a location you can measure distance from. Decide
how to do that (store latitude/longitude, or use a geocoding service) before building the Deployments module.

## 8. Security: what's in, what's left

**In place:** JWT verification (signature, expiry, issuer, audience; `alg=none`, tampered and
foreign-key tokens are rejected) · roles from the database, never from the token · Row Level Security deny-by-default ·
service key used only server-side · 1 MiB body limit, unknown JSON fields rejected · validated UUIDs and
parameterized queries (no SQL injection) · soft delete (archive) instead of hard delete · audit log ·
the public careers page hides client identity · errors never leak SQL or internals · logs never contain
request bodies · graceful shutdown · non-root containers.

**Still yours to do:** rate limiting on public endpoints (put it at the gateway or your host) · HTTPS
(your host or reverse proxy terminates TLS; never expose plain HTTP in production) · daily backups (NFR 5.2.1;
Supabase offers backups depending on plan) · file-upload rules for resumes and IDs (restrict types and size
in Supabase Storage policies; keep buckets private) · a data-retention policy for RA 10173 · set
`APP_ENV=production` when you deploy · the front-end's session-idle logout.

## 9. Troubleshooting

| Symptom | Fix |
|---|---|
| `missing go.sum entry` / `no required module provides package` | run `go mod tidy` |
| `missing required environment variables` | `.env` isn't in the folder you ran the command from, or a value is empty |
| `password authentication failed` | wrong DB password, or special characters not URL-encoded |
| `no route to host` / connection hangs on a `db.xxx.supabase.co` address | you used the IPv6-only direct connection; use the **Session pooler** string |
| `/readyz` returns 503 | the API can't reach the database; read the API terminal for the reason |
| `401` and log says `token is HS256 but SUPABASE_JWT_SECRET is not set` | set `SUPABASE_JWT_SECRET` in `.env` |
| `401` and log says `unknown signing key id` / `fetch JWKS` | `SUPABASE_URL` is wrong or unreachable |
| `403 no_profile` | the signup trigger wasn't installed: re-run `db/schema.sql`, then insert your profile or recreate the user |
| `403 forbidden` | your role isn't allowed on that route; check `select role from public.users` |
| `503 not_configured` on `POST /users` | set `SUPABASE_SERVICE_ROLE_KEY` |
| browser says CORS error | add your front-end's exact origin to `CORS_ALLOWED_ORIGINS` and restart the gateway |
| Docker build says `go.sum: not found` | run `go mod tidy` first |
