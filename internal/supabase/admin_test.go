package supabase

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateUser(t *testing.T) {
	var gotAuth, gotKey string
	var gotBody map[string]any
	status, resp := http.StatusOK, `{"id":"3f2b8c1e-9a4d-4e6f-8b1a-0c2d3e4f5a6b"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/auth/v1/admin/users" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		gotAuth, gotKey = r.Header.Get("Authorization"), r.Header.Get("apikey")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()

	u := NewUser{Email: "hr@example.com", Password: "a-long-password", FullName: "Hannah R.", Role: "hr"}

	// new-style secret key: apikey header only
	id, err := NewAdmin(srv.URL, "sb_secret_abc").CreateUser(context.Background(), u)
	if err != nil || id != "3f2b8c1e-9a4d-4e6f-8b1a-0c2d3e4f5a6b" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if gotKey != "sb_secret_abc" || gotAuth != "" {
		t.Errorf("secret key headers wrong: apikey=%q auth=%q", gotKey, gotAuth)
	}
	if meta, _ := gotBody["app_metadata"].(map[string]any); meta["role"] != "hr" || gotBody["email_confirm"] != true {
		t.Errorf("body wrong: %v", gotBody)
	}

	// legacy JWT service_role key: also sent as bearer
	if _, err := NewAdmin(srv.URL, "eyJhbGciOi.payload.sig").CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer eyJhbGciOi.payload.sig" {
		t.Errorf("legacy key not sent as bearer: %q", gotAuth)
	}

	status, resp = 422, `{"code":422,"error_code":"email_exists","msg":"already registered"}`
	if _, err := NewAdmin(srv.URL, "k").CreateUser(context.Background(), u); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("want ErrEmailTaken, got %v", err)
	}
	status, resp = 422, `{"code":422,"error_code":"weak_password","msg":"Password is too weak"}`
	var rej *RejectedError
	if _, err := NewAdmin(srv.URL, "k").CreateUser(context.Background(), u); !errors.As(err, &rej) || rej.Message != "Password is too weak" {
		t.Errorf("want RejectedError, got %v", err)
	}
	status, resp = 500, `boom`
	if _, err := NewAdmin(srv.URL, "k").CreateUser(context.Background(), u); err == nil || errors.As(err, &rej) {
		t.Errorf("500 should be a plain error, got %v", err)
	}
}

func TestEnabled(t *testing.T) {
	if NewAdmin("http://x", "").Enabled() || !NewAdmin("http://x", "k").Enabled() {
		t.Error("Enabled wrong")
	}
	var nilAdmin *Admin
	if nilAdmin.Enabled() {
		t.Error("nil Admin must be disabled")
	}
}
