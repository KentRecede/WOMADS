package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

type ctxKey struct{}

const RequestIDHeader = "X-Request-Id"

// RequestID tags every request with an id (reusing the gateway's, if sane),
// echoes it in the response, and exposes it to logs.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if len(id) == 0 || len(id) > 64 || !isToken(id) {
			id = NewRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

func NewRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func isToken(s string) bool {
	for _, c := range s {
		ok := c == '-' || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !ok {
			return false
		}
	}
	return true
}

// AccessLog writes one structured line per request. It never logs bodies or
// query strings, so personal data stays out of the logs.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("request",
			"method", r.Method, "path", r.URL.Path, "status", ww.Status(),
			"ms", time.Since(start).Milliseconds(), "request_id", RequestIDFrom(r.Context()))
	})
}

// Recover turns a panic into a JSON 500 instead of dropping the connection.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				slog.Error("panic", "value", rec, "path", r.URL.Path, "request_id", RequestIDFrom(r.Context()))
				Error(w, http.StatusInternalServerError, "internal_error", "Something went wrong. Please try again.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// APIHeaders adds conservative security headers to API responses.
func APIHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store") // responses contain personal data
		next.ServeHTTP(w, r)
	})
}
