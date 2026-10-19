// Package auth verifies Supabase Auth access tokens and enforces roles.
//
// How a request is authenticated:
//  1. The browser logs in with Supabase Auth and receives a signed JWT.
//  2. It sends the JWT to us as "Authorization: Bearer <token>".
//  3. Verifier checks the signature, expiry, issuer and audience.
//  4. Authenticate looks the user up in OUR users table to get their role.
//
// The role is never read from the token. Tokens carry user-editable
// metadata, so trusting them for authorization would let anyone promote
// themselves. The database is the only source of truth for roles.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the subset of Supabase's token payload we use.
type Claims struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
}

// Verifier validates Supabase access tokens. It accepts:
//   - HS256 tokens signed with your project's legacy JWT secret, and
//   - ES256 / RS256 tokens signed with an asymmetric key, whose public half is
//     fetched from the project's JWKS endpoint.
//
// The algorithm is chosen by the token header, but each algorithm can only be
// checked with its own kind of key, so a forged header cannot downgrade it.
type Verifier struct {
	secret []byte
	jwks   *jwksCache
	parser *jwt.Parser
}

func NewVerifier(supabaseURL, hmacSecret string) *Verifier {
	issuer := strings.TrimRight(supabaseURL, "/") + "/auth/v1"
	return &Verifier{
		secret: []byte(hmacSecret),
		jwks:   newJWKSCache(issuer+"/.well-known/jwks.json", &http.Client{Timeout: 5 * time.Second}),
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{"HS256", "ES256", "RS256"}),
			jwt.WithAudience("authenticated"),
			jwt.WithIssuer(issuer),
			jwt.WithExpirationRequired(),
			jwt.WithLeeway(30*time.Second),
		),
	}
}

// Verify returns the token's claims if it is valid, or an error otherwise.
func (v *Verifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	claims := &Claims{}
	_, err := v.parser.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		switch t.Method.Alg() {
		case "HS256":
			if len(v.secret) == 0 {
				return nil, errors.New("token is HS256 but SUPABASE_JWT_SECRET is not set")
			}
			return v.secret, nil
		case "ES256", "RS256":
			kid, _ := t.Header["kid"].(string)
			if kid == "" {
				return nil, errors.New("token has no key id (kid)")
			}
			return v.jwks.key(ctx, kid)
		default:
			return nil, fmt.Errorf("unsupported algorithm %q", t.Method.Alg())
		}
	})
	if err != nil {
		return nil, err
	}
	if claims.Subject == "" {
		return nil, errors.New("token has no subject")
	}
	return claims, nil
}
