package server

// End-to-end tests: real router + real PostgreSQL + real JWT checks.
//
// They are skipped unless TEST_DATABASE_URL is set. Point it at a SCRATCH
// database that has db/dev/auth_stub.sql and db/schema.sql applied (see
// README.md, "Run the integration tests"). Never point it at production data:
// the tests insert and delete rows.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"womads/internal/auth"
	"womads/internal/config"
	"womads/internal/database"
	"womads/internal/models"
	"womads/internal/supabase"
)

const (
	itSupabaseURL = "https://test.supabase.co"
	itSecret      = "integration-test-secret-32-bytes-min!!"
)

type harness struct {
	t       *testing.T
	db      *gorm.DB
	router  http.Handler
	users   []string
	clients []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration tests")
	}
	db, err := database.Connect(dsn)
	if err != nil {
		t.Fatal(err)
	}
	hn := &harness{t: t, db: db}

	// A fake Supabase Auth admin API that inserts into the stand-in auth.users
	// table (which fires the same trigger the real one does).
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Email        string         `json:"email"`
			AppMetadata  map[string]any `json:"app_metadata"`
			UserMetadata map[string]any `json:"user_metadata"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		var n int64
		db.Raw("select count(*) from auth.users where lower(email) = lower(?)", in.Email).Scan(&n)
		if n > 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"error_code":"email_exists","msg":"already registered"}`))
			return
		}
		app, _ := json.Marshal(in.AppMetadata)
		usr, _ := json.Marshal(in.UserMetadata)
		var id string
		db.Raw("insert into auth.users (email, raw_app_meta_data, raw_user_meta_data) values (?, ?::jsonb, ?::jsonb) returning id",
			in.Email, string(app), string(usr)).Scan(&id)
		hn.users = append(hn.users, id)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
	})
	fake := httptest.NewServer(mux)
	t.Cleanup(fake.Close)

	hn.router = NewRouter(Deps{
		Cfg:      config.API{Env: "development"},
		DB:       db,
		Verifier: auth.NewVerifier(itSupabaseURL, itSecret),
		Profiles: auth.GormProfiles{DB: db},
		Admin:    supabase.NewAdmin(fake.URL, "test-service-key"),
	})
	t.Cleanup(hn.cleanup)
	return hn
}

func (hn *harness) cleanup() {
	db := hn.db
	for _, id := range hn.clients {
		db.Exec("delete from deployments where client_id = ?", id)
		db.Exec("delete from employees where client_id = ?", id)
		db.Exec("delete from job_openings where client_id = ?", id)
		db.Exec("delete from clients where client_id = ?", id)
	}
	for _, id := range hn.users {
		db.Exec("delete from audit_logs where actor_id = ? or entity_id = ?", id, id)
		db.Exec("delete from auth.users where id = ?", id) // cascades to public.users
	}
}

// newUser creates a login with the given role and returns its id and a valid token.
func (hn *harness) newUser(role string) (id, token string) {
	hn.t.Helper()
	meta, _ := json.Marshal(map[string]string{"role": role})
	email := fmt.Sprintf("it-%d-%s@example.test", time.Now().UnixNano(), role)
	if err := hn.db.Raw("insert into auth.users (email, raw_app_meta_data) values (?, ?::jsonb) returning id",
		email, string(meta)).Scan(&id).Error; err != nil || id == "" {
		hn.t.Fatalf("create user: %v", err)
	}
	hn.users = append(hn.users, id)
	return id, mint(hn.t, id)
}

func mint(t *testing.T, sub string) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": sub, "aud": "authenticated", "iss": itSupabaseURL + "/auth/v1",
		"exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(itSecret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type resp struct {
	code int
	body map[string]any
}

func (hn *harness) call(method, path, token string, body any) resp {
	hn.t.Helper()
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case string:
		rd = bytes.NewReader([]byte(b))
	default:
		j, _ := json.Marshal(b)
		rd = bytes.NewReader(j)
	}
	req := httptest.NewRequest(method, path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	hn.router.ServeHTTP(rec, req)
	out := resp{code: rec.Code}
	_ = json.Unmarshal(rec.Body.Bytes(), &out.body)
	return out
}

func (r resp) str(keys ...string) string {
	var cur any = r.body
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[k]
	}
	s, _ := cur.(string)
	return s
}

func (r resp) expect(t *testing.T, want int) resp {
	t.Helper()
	if r.code != want {
		t.Fatalf("status = %d, want %d; body = %v", r.code, want, r.body)
	}
	return r
}

func TestAccessControl(t *testing.T) {
	hn := newHarness(t)
	_, admin := hn.newUser(models.RoleAdministrator)
	_, hr := hn.newUser(models.RoleHR)
	_, payroll := hn.newUser(models.RolePayrollOfficer)
	_, applicant := hn.newUser(models.RoleApplicant)

	hn.call("GET", "/api/v1/clients", "", nil).expect(t, 401)
	hn.call("GET", "/api/v1/clients", "garbage", nil).expect(t, 401)

	if got := hn.call("GET", "/api/v1/me", hr, nil).expect(t, 200).str("data", "role"); got != "hr" {
		t.Errorf("/me role = %q", got)
	}
	hn.call("GET", "/api/v1/clients", applicant, nil).expect(t, 403)
	hn.call("GET", "/api/v1/clients", payroll, nil).expect(t, 403)
	hn.call("GET", "/api/v1/clients", hr, nil).expect(t, 200)
	hn.call("GET", "/api/v1/users", hr, nil).expect(t, 403)
	hn.call("GET", "/api/v1/users", admin, nil).expect(t, 200)

	// Planned modules: guarded now, unimplemented until built.
	hn.call("GET", "/api/v1/payroll", payroll, nil).expect(t, 501)
	hn.call("GET", "/api/v1/payroll", hr, nil).expect(t, 403)
	hn.call("POST", "/api/v1/applicants/3f2b8c1e-9a4d-4e6f-8b1a-0c2d3e4f5a6b/approve", hr, nil).expect(t, 501)
	hn.call("POST", "/api/v1/applicants/3f2b8c1e-9a4d-4e6f-8b1a-0c2d3e4f5a6b/approve", admin, nil).expect(t, 403) // business rule 2: HR only

	// A forged role in a token's metadata must not matter: roles come from the database.
	id, _ := hn.newUser(models.RoleApplicant)
	forged, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": id, "aud": "authenticated", "iss": itSupabaseURL + "/auth/v1",
		"exp": time.Now().Add(time.Hour).Unix(), "role": "administrator",
		"user_metadata": map[string]string{"role": "administrator"},
	}).SignedString([]byte(itSecret))
	hn.call("GET", "/api/v1/users", forged, nil).expect(t, 403)
}

func TestClientsLifecycle(t *testing.T) {
	hn := newHarness(t)
	_, hr := hn.newUser(models.RoleHR)

	good := map[string]any{"company_name": "Acme Foods Inc.", "contact_person": "J. Reyes",
		"number_of_workers": 2, "start_date": "2026-11-01", "end_date": "2027-10-31"}

	// validation gateway
	bad := hn.call("POST", "/api/v1/clients", hr, map[string]any{"company_name": " ", "number_of_workers": 0,
		"start_date": "2027-01-01", "end_date": "2026-01-01"}).expect(t, 422)
	fields, _ := bad.body["error"].(map[string]any)["fields"].(map[string]any)
	for _, f := range []string{"company_name", "contact_person", "number_of_workers", "end_date"} {
		if fields[f] == nil {
			t.Errorf("expected a validation message for %q, got %v", f, fields)
		}
	}
	hn.call("POST", "/api/v1/clients", hr, `{"company_name":"x","is_admin":true}`).expect(t, 400) // unknown field
	hn.call("POST", "/api/v1/clients", hr, map[string]any{"start_date": "01/11/2026"}).expect(t, 400)

	created := hn.call("POST", "/api/v1/clients", hr, good).expect(t, 201)
	id := created.str("data", "id")
	hn.clients = append(hn.clients, id)
	if created.str("data", "start_date") != "2026-11-01" || created.str("data", "end_date") != "2027-10-31" {
		t.Errorf("dates did not round-trip: %v", created.body["data"])
	}

	list := hn.call("GET", "/api/v1/clients?q=acme+foods", hr, nil).expect(t, 200)
	if total, _ := list.body["meta"].(map[string]any)["total"].(float64); total < 1 {
		t.Errorf("search did not find the client: %v", list.body)
	}
	hn.call("GET", "/api/v1/clients/not-a-uuid", hr, nil).expect(t, 404)

	// partial update, validated against the merged record
	upd := hn.call("PATCH", "/api/v1/clients/"+id, hr, map[string]any{"contact_person": "M. Cruz"}).expect(t, 200)
	if upd.str("data", "contact_person") != "M. Cruz" || upd.str("data", "company_name") != "Acme Foods Inc." {
		t.Errorf("partial update wrong: %v", upd.body["data"])
	}
	hn.call("PATCH", "/api/v1/clients/"+id, hr, map[string]any{"end_date": "2026-01-01"}).expect(t, 422)

	// deployments + quota + delete protection
	var empID string
	hn.db.Raw("insert into employees (name, position, client_id) values ('IT Worker', 'Packer', ?) returning employee_id", id).Scan(&empID)
	var depID string
	hn.db.Raw("insert into deployments (employee_id, client_id, start_date) values (?, ?, '2026-11-01') returning deployment_id", empID, id).Scan(&depID)

	dep := hn.call("GET", "/api/v1/clients/"+id+"/deployments", hr, nil).expect(t, 200)
	rows, _ := dep.body["data"].([]any)
	meta, _ := dep.body["meta"].(map[string]any)
	if len(rows) != 1 || meta["quota"] != float64(2) || meta["deployed"] != float64(1) {
		t.Errorf("deployments view wrong: %v", dep.body)
	}
	hn.call("DELETE", "/api/v1/clients/"+id, hr, nil).expect(t, 409)

	hn.db.Exec("update deployments set status = 'completed' where deployment_id = ?", depID)
	hn.call("DELETE", "/api/v1/clients/"+id, hr, nil).expect(t, 204)
	hn.call("GET", "/api/v1/clients/"+id, hr, nil).expect(t, 404)
	var archived int64 // soft delete: the row is archived, not destroyed
	hn.db.Raw("select count(*) from clients where client_id = ? and deleted_at is not null", id).Scan(&archived)
	if archived != 1 {
		t.Error("client should be soft-deleted, not removed")
	}
}

func TestJobOpeningLifecycle(t *testing.T) {
	hn := newHarness(t)
	_, hr := hn.newUser(models.RoleHR)
	client := hn.call("POST", "/api/v1/clients", hr, map[string]any{"company_name": "Job Test Co", "contact_person": "X",
		"number_of_workers": 5, "start_date": "2026-11-01", "end_date": "2027-10-31"}).expect(t, 201)
	cid := client.str("data", "id")
	hn.clients = append(hn.clients, cid)

	hn.call("POST", "/api/v1/jobs", hr, map[string]any{"client_id": "3f2b8c1e-9a4d-4e6f-8b1a-0c2d3e4f5a6b", "job_title": "Ghost"}).expect(t, 422)

	job := hn.call("POST", "/api/v1/jobs", hr, map[string]any{"client_id": cid, "job_title": "Machine Operator"}).expect(t, 201)
	jid := job.str("data", "id")
	if job.str("data", "status") != "draft" {
		t.Errorf("new job should be a draft, got %q", job.str("data", "status"))
	}

	hn.call("POST", "/api/v1/jobs/"+jid+"/publish", hr, nil).expect(t, 422) // incomplete listing
	hn.call("GET", "/api/v1/public/jobs/"+jid, "", nil).expect(t, 404)      // drafts are invisible

	hn.call("PATCH", "/api/v1/jobs/"+jid, hr, map[string]any{"job_description": "Operate packaging line.",
		"location": "Mandaue City", "category": "Production"}).expect(t, 200)
	hn.call("POST", "/api/v1/jobs/"+jid+"/publish", hr, nil).expect(t, 200)
	hn.call("POST", "/api/v1/jobs/"+jid+"/publish", hr, nil).expect(t, 200) // idempotent
	hn.call("DELETE", "/api/v1/jobs/"+jid, hr, nil).expect(t, 409)          // must close first

	// public careers page: no login, and it must not reveal the client
	pub := hn.call("GET", "/api/v1/public/jobs?category=production", "", nil).expect(t, 200)
	items, _ := pub.body["data"].([]any)
	found := false
	for _, it := range items {
		m := it.(map[string]any)
		if m["id"] == jid {
			found = true
			if _, leaked := m["client_id"]; leaked {
				t.Error("public job leaks client_id")
			}
			if _, leaked := m["posted_by"]; leaked {
				t.Error("public job leaks posted_by")
			}
		}
	}
	if !found {
		t.Errorf("published job missing from public list: %v", pub.body)
	}
	hn.call("GET", "/api/v1/public/jobs/"+jid, "", nil).expect(t, 200)

	hn.call("POST", "/api/v1/jobs/"+jid+"/close", hr, nil).expect(t, 200)
	hn.call("GET", "/api/v1/public/jobs/"+jid, "", nil).expect(t, 404)
	hn.call("PATCH", "/api/v1/jobs/"+jid, hr, map[string]any{"job_title": "Changed"}).expect(t, 409)
	hn.call("POST", "/api/v1/jobs/"+jid+"/publish", hr, nil).expect(t, 409)
	hn.call("DELETE", "/api/v1/jobs/"+jid, hr, nil).expect(t, 204)
	hn.call("GET", "/api/v1/public/jobs/"+jid, "", nil).expect(t, 404)
}

func TestAccountManagement(t *testing.T) {
	hn := newHarness(t)
	adminID, admin := hn.newUser(models.RoleAdministrator)

	email := fmt.Sprintf("new-hr-%d@example.test", time.Now().UnixNano())
	in := map[string]any{"email": email, "password": "correct-horse-battery", "user_name": "Hannah R.", "role": "hr"}

	hn.call("POST", "/api/v1/users", admin, map[string]any{"email": "nope", "password": "short", "user_name": "", "role": "king"}).expect(t, 422)

	created := hn.call("POST", "/api/v1/users", admin, in).expect(t, 201)
	newID := created.str("data", "id")
	if created.str("data", "role") != "hr" || created.str("data", "email") != email {
		t.Errorf("created user wrong: %v", created.body["data"])
	}
	if _, leaked := created.body["data"].(map[string]any)["password"]; leaked {
		t.Error("response must never contain a password")
	}
	hn.call("POST", "/api/v1/users", admin, in).expect(t, 409) // email already registered

	// the new account can sign in as HR
	hn.call("GET", "/api/v1/me", mint(t, newID), nil).expect(t, 200)

	// guard rails: an admin can't lock themselves out
	hn.call("PATCH", "/api/v1/users/"+adminID, admin, map[string]any{"role": "hr"}).expect(t, 409)
	hn.call("PATCH", "/api/v1/users/"+adminID, admin, map[string]any{"is_active": false}).expect(t, 409)
	hn.call("DELETE", "/api/v1/users/"+adminID, admin, nil).expect(t, 409)
	hn.call("PATCH", "/api/v1/users/"+adminID, admin, map[string]any{"user_name": "Renamed Admin"}).expect(t, 200)
	hn.call("PATCH", "/api/v1/users/"+newID, admin, map[string]any{}).expect(t, 422)

	// deactivate -> blocked; reactivate -> allowed; delete -> no profile
	hn.call("PATCH", "/api/v1/users/"+newID, admin, map[string]any{"is_active": false}).expect(t, 200)
	hn.call("GET", "/api/v1/me", mint(t, newID), nil).expect(t, 403)
	hn.call("PATCH", "/api/v1/users/"+newID, admin, map[string]any{"is_active": true, "role": "payroll_officer"}).expect(t, 200)
	if got := hn.call("GET", "/api/v1/me", mint(t, newID), nil).expect(t, 200).str("data", "role"); got != "payroll_officer" {
		t.Errorf("role change not applied, got %q", got)
	}
	hn.call("DELETE", "/api/v1/users/"+newID, admin, nil).expect(t, 204)
	hn.call("GET", "/api/v1/me", mint(t, newID), nil).expect(t, 403)
	hn.call("GET", "/api/v1/users/"+newID, admin, nil).expect(t, 404)

	// every change above was audited
	var n int64
	hn.db.Raw("select count(*) from audit_logs where actor_id = ?", adminID).Scan(&n)
	if n < 4 {
		t.Errorf("expected audit entries for the admin's actions, found %d", n)
	}
}
