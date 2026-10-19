package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testSecret = "super-secret-test-value-at-least-32-bytes!"
	userID     = "3f2b8c1e-9a4d-4e6f-8b1a-0c2d3e4f5a6b"
)

func claimsFor(iss string, mutate func(jwt.MapClaims)) jwt.MapClaims {
	c := jwt.MapClaims{
		"sub": userID, "aud": "authenticated", "iss": iss,
		"email": "hr@example.com", "role": "authenticated",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	}
	if mutate != nil {
		mutate(c)
	}
	return c
}

func signHS256(t *testing.T, c jwt.MapClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestVerifyHS256(t *testing.T) {
	const base = "https://proj.supabase.co"
	iss := base + "/auth/v1"
	v := NewVerifier(base, testSecret)

	if _, err := v.Verify(context.Background(), signHS256(t, claimsFor(iss, nil))); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}

	bad := map[string]jwt.MapClaims{
		"expired":        claimsFor(iss, func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }),
		"no expiry":      claimsFor(iss, func(c jwt.MapClaims) { delete(c, "exp") }),
		"wrong audience": claimsFor(iss, func(c jwt.MapClaims) { c["aud"] = "anon" }),
		"wrong issuer":   claimsFor("https://evil.example/auth/v1", nil),
		"no subject":     claimsFor(iss, func(c jwt.MapClaims) { delete(c, "sub") }),
	}
	for name, c := range bad {
		if _, err := v.Verify(context.Background(), signHS256(t, c)); err == nil {
			t.Errorf("%s: token was accepted", name)
		}
	}

	// tampered signature
	tok := signHS256(t, claimsFor(iss, nil))
	if _, err := v.Verify(context.Background(), tok[:len(tok)-3]+"abc"); err == nil {
		t.Error("tampered token accepted")
	}

	// alg=none must never work
	none, _ := jwt.NewWithClaims(jwt.SigningMethodNone, claimsFor(iss, nil)).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, err := v.Verify(context.Background(), none); err == nil {
		t.Error("alg=none token accepted")
	}

	// HS256 token but no secret configured (project uses asymmetric keys only)
	if _, err := NewVerifier(base, "").Verify(context.Background(), signHS256(t, claimsFor(iss, nil))); err == nil {
		t.Error("HS256 token accepted with no secret configured")
	}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func TestVerifyAsymmetricViaJWKS(t *testing.T) {
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)

	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/v1/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{
			{"kty": "EC", "crv": "P-256", "kid": "ec-1", "x": b64(ecKey.X.Bytes()), "y": b64(ecKey.Y.Bytes())},
			{"kty": "RSA", "kid": "rsa-1", "n": b64(rsaKey.N.Bytes()), "e": b64(big.NewInt(int64(rsaKey.E)).Bytes())},
			{"kty": "oct", "kid": "ignored", "k": "xxx"},
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	v := NewVerifier(srv.URL, "")
	iss := srv.URL + "/auth/v1"

	sign := func(m jwt.SigningMethod, key any, kid string) string {
		tok := jwt.NewWithClaims(m, claimsFor(iss, nil))
		if kid != "" {
			tok.Header["kid"] = kid
		}
		s, err := tok.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	if _, err := v.Verify(context.Background(), sign(jwt.SigningMethodES256, ecKey, "ec-1")); err != nil {
		t.Errorf("ES256 rejected: %v", err)
	}
	if _, err := v.Verify(context.Background(), sign(jwt.SigningMethodRS256, rsaKey, "rsa-1")); err != nil {
		t.Errorf("RS256 rejected: %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("JWKS fetched %d times, want 1 (cached)", hits.Load())
	}

	// signed by a key we don't know, claiming a known kid
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if _, err := v.Verify(context.Background(), sign(jwt.SigningMethodES256, other, "ec-1")); err == nil {
		t.Error("token signed with a foreign key accepted")
	}
	// unknown kid, and no kid at all
	if _, err := v.Verify(context.Background(), sign(jwt.SigningMethodES256, ecKey, "nope")); err == nil {
		t.Error("unknown kid accepted")
	}
	if _, err := v.Verify(context.Background(), sign(jwt.SigningMethodES256, ecKey, "")); err == nil {
		t.Error("missing kid accepted")
	}
	// repeated bogus kids must not trigger a refetch storm
	if hits.Load() != 1 {
		t.Errorf("JWKS refetched %d times for bogus kids, want 1", hits.Load())
	}
}

// ---- middleware -----------------------------------------------------------

type fakeStore struct {
	p   *Principal
	err error
}

func (f fakeStore) ByID(context.Context, string) (*Principal, error) { return f.p, f.err }

func TestAuthenticateAndRoles(t *testing.T) {
	const base = "https://proj.supabase.co"
	v := NewVerifier(base, testSecret)
	good := signHS256(t, claimsFor(base+"/auth/v1", nil))

	run := func(store ProfileStore, header string, roles ...string) int {
		h := Authenticate(v, store)(RequireRoles(roles...)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if PrincipalFrom(r.Context()) == nil {
				t.Error("principal missing in handler")
			}
			w.WriteHeader(http.StatusNoContent)
		})))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	hr := fakeStore{p: &Principal{ID: userID, Role: "hr", Active: true}}
	cases := []struct {
		name   string
		store  ProfileStore
		header string
		roles  []string
		want   int
	}{
		{"no header", hr, "", []string{"hr"}, 401},
		{"wrong scheme", hr, "Basic abc", []string{"hr"}, 401},
		{"garbage token", hr, "Bearer not.a.jwt", []string{"hr"}, 401},
		{"valid + allowed role", hr, "Bearer " + good, []string{"hr", "administrator"}, 204},
		{"lowercase scheme ok", hr, "bearer " + good, []string{"hr"}, 204},
		{"valid but wrong role", hr, "Bearer " + good, []string{"administrator"}, 403},
		{"no profile row", fakeStore{err: ErrNoProfile}, "Bearer " + good, []string{"hr"}, 403},
		{"deactivated account", fakeStore{p: &Principal{ID: userID, Role: "hr", Active: false}}, "Bearer " + good, []string{"hr"}, 403},
	}
	for _, c := range cases {
		if got := run(c.store, c.header, c.roles...); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}
