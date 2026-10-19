package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"womads/internal/httpx"
	"womads/internal/models"
)

// Principal is the authenticated caller, as stored in our users table.
type Principal struct {
	ID       string
	Email    string
	UserName string
	Role     string
	Active   bool
}

var ErrNoProfile = errors.New("no profile for this account")

// ProfileStore loads a Principal by Supabase user id.
// It is an interface so tests can run without a database.
type ProfileStore interface {
	ByID(ctx context.Context, id string) (*Principal, error)
}

// GormProfiles is the production ProfileStore.
type GormProfiles struct{ DB *gorm.DB }

func (g GormProfiles) ByID(ctx context.Context, id string) (*Principal, error) {
	var u models.User // soft-deleted users are excluded automatically
	err := g.DB.WithContext(ctx).First(&u, "user_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNoProfile
	}
	if err != nil {
		return nil, err
	}
	return &Principal{ID: u.ID, Email: u.Email, UserName: u.UserName, Role: u.Role, Active: u.IsActive}, nil
}

type principalKey struct{}

// PrincipalFrom returns the caller, or nil on routes without Authenticate.
func PrincipalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}

// Authenticate requires a valid Supabase token and an active profile.
// 401 = we don't know who you are. 403 = we know, but you may not come in.
func Authenticate(v *Verifier, store ProfileStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := bearerToken(r)
			if !ok {
				unauthorized(w, "Missing bearer token.")
				return
			}
			claims, err := v.Verify(r.Context(), raw)
			if err == nil && !httpx.IsUUID(claims.Subject) {
				err = errors.New("subject is not a UUID")
			}
			if err != nil {
				slog.Warn("auth: token rejected", "reason", err.Error(), "request_id", httpx.RequestIDFrom(r.Context()))
				unauthorized(w, "Invalid or expired token.")
				return
			}

			p, err := store.ByID(r.Context(), strings.ToLower(claims.Subject))
			switch {
			case errors.Is(err, ErrNoProfile):
				httpx.Error(w, http.StatusForbidden, "no_profile", "This account has no profile. Contact an administrator.")
				return
			case err != nil:
				httpx.Internal(w, r, err)
				return
			case !p.Active:
				slog.Warn("auth: disabled account blocked", "user_id", p.ID)
				httpx.Error(w, http.StatusForbidden, "account_disabled", "This account has been deactivated.")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
		})
	}
}

// RequireRoles allows only the listed roles. Use it after Authenticate.
func RequireRoles(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := PrincipalFrom(r.Context())
			if p == nil {
				unauthorized(w, "Authentication required.")
				return
			}
			if _, ok := allowed[p.Role]; !ok {
				slog.Warn("auth: role denied", "user_id", p.ID, "role", p.Role, "method", r.Method, "path", r.URL.Path)
				httpx.Error(w, http.StatusForbidden, "forbidden", "You do not have permission to do this.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "bearer") || token == "" {
		return "", false
	}
	return token, true
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="womads"`)
	httpx.Error(w, http.StatusUnauthorized, "unauthorized", msg)
}
