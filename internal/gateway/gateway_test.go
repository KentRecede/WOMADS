package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"womads/internal/config"
)

func TestGateway(t *testing.T) {
	var got struct{ path, xff, reqID string }
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.xff, got.reqID = r.URL.RequestURI(), r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Request-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"from":"core"}`))
	}))
	defer core.Close()

	h, err := New(config.Gateway{CoreURL: core.URL, AllowedOrigins: []string{"http://localhost:3000"}})
	if err != nil {
		t.Fatal(err)
	}
	do := func(method, path string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = "203.0.113.9:5555"
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	t.Run("health is answered locally", func(t *testing.T) {
		got.path = ""
		if rec := do("GET", "/healthz", nil); rec.Code != 200 || got.path != "" {
			t.Errorf("code=%d upstream=%q", rec.Code, got.path)
		}
	})

	t.Run("api is proxied with path and query intact", func(t *testing.T) {
		rec := do("GET", "/api/v1/clients?q=acme&page=2", nil)
		if rec.Code != 200 || got.path != "/api/v1/clients?q=acme&page=2" || !strings.Contains(rec.Body.String(), "core") {
			t.Errorf("code=%d path=%q body=%s", rec.Code, got.path, rec.Body)
		}
	})

	t.Run("forged X-Forwarded-For is replaced", func(t *testing.T) {
		do("GET", "/api/v1/me", map[string]string{"X-Forwarded-For": "6.6.6.6"})
		if got.xff != "203.0.113.9" {
			t.Errorf("upstream saw X-Forwarded-For=%q, want the real client IP only", got.xff)
		}
	})

	t.Run("request id is generated and forwarded", func(t *testing.T) {
		rec := do("GET", "/api/v1/me", nil)
		if got.reqID == "" || rec.Header().Get("X-Request-Id") != got.reqID {
			t.Errorf("upstream=%q response=%q", got.reqID, rec.Header().Get("X-Request-Id"))
		}
	})

	t.Run("unknown paths are not proxied", func(t *testing.T) {
		got.path = ""
		if rec := do("GET", "/admin", nil); rec.Code != 404 || got.path != "" {
			t.Errorf("code=%d upstream=%q", rec.Code, got.path)
		}
	})

	t.Run("cors: allowed origin", func(t *testing.T) {
		rec := do("OPTIONS", "/api/v1/clients", map[string]string{
			"Origin": "http://localhost:3000", "Access-Control-Request-Method": "POST"})
		if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" ||
			!strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
			t.Errorf("preflight: code=%d headers=%v", rec.Code, rec.Header())
		}
		rec = do("GET", "/api/v1/me", map[string]string{"Origin": "http://localhost:3000"})
		if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
			t.Error("actual request missing CORS header")
		}
	})

	t.Run("cors: foreign origin", func(t *testing.T) {
		rec := do("OPTIONS", "/api/v1/clients", map[string]string{
			"Origin": "https://evil.example", "Access-Control-Request-Method": "POST"})
		if rec.Code != 403 || rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("preflight: code=%d allow=%q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
		}
		rec = do("GET", "/api/v1/me", map[string]string{"Origin": "https://evil.example"})
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("foreign origin was granted CORS")
		}
	})

	t.Run("upstream down gives a JSON 502", func(t *testing.T) {
		dead, _ := New(config.Gateway{CoreURL: "http://127.0.0.1:1"})
		rec := httptest.NewRecorder()
		dead.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/me", nil))
		var body struct{ Error struct{ Code string } }
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != 502 || body.Error.Code != "upstream_unavailable" {
			t.Errorf("code=%d body=%s", rec.Code, rec.Body)
		}
	})
}
