package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decode(t *testing.T, body string) (ok bool, code int) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	var dst struct {
		Name string `json:"name"`
	}
	ok = DecodeJSON(rec, req, &dst)
	return ok, rec.Code
}

func TestDecodeJSON(t *testing.T) {
	if ok, _ := decode(t, `{"name":"x"}`); !ok {
		t.Error("valid body rejected")
	}
	for name, body := range map[string]string{
		"unknown field": `{"name":"x","admin":true}`,
		"trailing json": `{"name":"x"}{"name":"y"}`,
		"empty":         ``,
		"malformed":     `{"name":`,
	} {
		if ok, code := decode(t, body); ok || code != http.StatusBadRequest {
			t.Errorf("%s: ok=%v code=%d, want rejected with 400", name, ok, code)
		}
	}
	big := `{"name":"` + strings.Repeat("a", maxBodyBytes) + `"}`
	if ok, code := decode(t, big); ok || code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize: ok=%v code=%d, want 413", ok, code)
	}
}

func TestParsePage(t *testing.T) {
	cases := map[string]Page{
		"":                       {1, 20},
		"page=3&page_size=50":    {3, 50},
		"page=0&page_size=-5":    {1, 20},
		"page_size=100000":       {1, 100},
		"page=abc&page_size=zzz": {1, 20},
	}
	for q, want := range cases {
		got := ParsePage(httptest.NewRequest(http.MethodGet, "/?"+q, nil))
		if got != want {
			t.Errorf("%q: got %+v want %+v", q, got, want)
		}
	}
	if (Page{3, 50}).Offset() != 100 {
		t.Error("offset wrong")
	}
}

func TestHelpers(t *testing.T) {
	if !IsUUID("3f2b8c1e-9a4d-4e6f-8b1a-0c2d3e4f5a6b") || IsUUID("123") || IsUUID("3f2b8c1e-9a4d-4e6f-8b1a-0c2d3e4f5a6b'; drop") {
		t.Error("IsUUID wrong")
	}
	if got := EscapeLike(`50%_off\`); got != `50\%\_off\\` {
		t.Errorf("EscapeLike = %q", got)
	}
	v := NewValidator()
	v.Text("a", "  ", 5)
	v.Text("b", "toolong", 3)
	v.Text("c", "ok", 5)
	if v.OK() || len(v.Fields) != 2 {
		t.Errorf("validator: %+v", v.Fields)
	}
}

func TestRequestIDRejectsJunk(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = RequestIDFrom(r.Context()) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "bad id\r\nX-Evil: 1")
	h.ServeHTTP(rec, req)
	if seen == "bad id\r\nX-Evil: 1" || len(seen) != 16 || rec.Header().Get(RequestIDHeader) != seen {
		t.Errorf("request id not sanitised: %q", seen)
	}
}
