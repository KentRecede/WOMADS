// Package gateway is the single front door described in the paper: the
// Next.js front-end talks only to the gateway, and the gateway routes each
// request to the right back-end service.
//
// Today every route goes to the core API. When you split a module into its
// own service, add one line to the routes table in New.
//
// The gateway does NOT check logins; each service verifies the token itself.
// It does three things every service would otherwise repeat:
//   - CORS, so the browser front-end is allowed to call the API
//   - request ids, so one request can be followed through every log
//   - it overwrites X-Forwarded-For with the real client address, so the
//     audit log can't be fooled by a forged header
package gateway

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"womads/internal/config"
	"womads/internal/httpx"
)

type route struct {
	pattern string
	target  *url.URL
}

func New(cfg config.Gateway) (http.Handler, error) {
	core, err := url.Parse(cfg.CoreURL)
	if err != nil {
		return nil, err
	}

	// Add one line per service here, e.g. {"/api/v1/payroll/*", payrollURL}.
	// The most specific pattern wins, so order doesn't matter.
	routes := []route{
		{"/api/*", core},
		{"/docs", core},
		{"/openapi.yaml", core},
		{"/readyz", core},
	}

	r := chi.NewRouter()
	r.Use(httpx.RequestID, httpx.Recover, httpx.AccessLog, cors(cfg.AllowedOrigins))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	for _, rt := range routes {
		r.Handle(rt.pattern, proxyTo(rt.target))
	}
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		httpx.Error(w, http.StatusNotFound, "not_found", "No such route.")
	})
	return r, nil
}

func proxyTo(target *url.URL) http.Handler {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second

	return &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded() // replaces any X-Forwarded-* the client sent
			pr.Out.Header.Set(httpx.RequestIDHeader, httpx.RequestIDFrom(pr.In.Context()))
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			httpx.Error(w, http.StatusBadGateway, "upstream_unavailable", "The service is temporarily unavailable.")
		},
	}
}

// cors allows only the listed browser origins (CORS_ALLOWED_ORIGINS).
// Requests without an Origin header (curl, server-to-server) are unaffected.
// We use bearer tokens rather than cookies, so credentials mode is not enabled.
func cors(allowed []string) func(http.Handler) http.Handler {
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		set[strings.TrimRight(o, "/")] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Add("Vary", "Origin")
			_, ok := set[origin]
			if ok {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Expose-Headers", httpx.RequestIDHeader)
			}
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				if !ok {
					httpx.Error(w, http.StatusForbidden, "cors_denied", "This origin is not allowed.")
					return
				}
				h := w.Header()
				h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, "+httpx.RequestIDHeader)
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
