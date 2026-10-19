// Package server wires every module into one HTTP router.
//
// Read this file top to bottom to see the whole API surface and who may call what:
//
//	public            no login        careers page
//	authenticated     any signed-in   /me, notifications
//	administrator     role guard      account management
//	administrator+hr  role guard      clients, job openings
//	...               stubs           see stubs.go
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"womads/internal/auth"
	"womads/internal/config"
	"womads/internal/docs"
	"womads/internal/httpx"
	"womads/internal/models"
	"womads/internal/modules/accounts"
	"womads/internal/modules/clients"
	"womads/internal/modules/jobs"
	"womads/internal/supabase"
)

type Deps struct {
	Cfg      config.API
	DB       *gorm.DB
	Verifier *auth.Verifier
	Profiles auth.ProfileStore
	Admin    *supabase.Admin
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.RequestID, httpx.Recover, httpx.AccessLog, httpx.APIHeaders)

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		httpx.Error(w, http.StatusNotFound, "not_found", "No such route.")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		httpx.Error(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed on this route.")
	})

	// Probes: /healthz = "process is up", /readyz = "and the database answers".
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", ready(d.DB))
	if !d.Cfg.IsProduction() {
		docs.Mount(r)
	}

	accountsH := &accounts.Handler{DB: d.DB, Admin: d.Admin}
	clientsH := &clients.Handler{DB: d.DB}
	jobsH := &jobs.Handler{DB: d.DB}

	r.Route("/api/v1", func(r chi.Router) {
		// ---- public ----
		r.Route("/public/jobs", jobsH.Public)

		// ---- everything below needs a valid token and an active profile ----
		r.Group(func(r chi.Router) {
			r.Use(auth.Authenticate(d.Verifier, d.Profiles))

			r.Get("/me", me)

			r.Group(func(r chi.Router) {
				r.Use(auth.RequireRoles(models.RoleAdministrator))
				r.Route("/users", accountsH.Routes)
			})

			r.Group(func(r chi.Router) {
				r.Use(auth.RequireRoles(models.RoleAdministrator, models.RoleHR))
				r.Route("/clients", clientsH.Routes)
				r.Route("/jobs", jobsH.Staff)
			})

			mountPlanned(r)
		})
	})
	return r
}

func me(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	httpx.Data(w, http.StatusOK, map[string]any{
		"id": p.ID, "email": p.Email, "user_name": p.UserName, "role": p.Role,
	})
}

func ready(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		sqlDB, err := db.DB()
		if err == nil {
			err = sqlDB.PingContext(ctx)
		}
		if err != nil {
			slog.Error("readiness check failed", "err", err)
			httpx.Error(w, http.StatusServiceUnavailable, "not_ready", "Database is unreachable.")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
