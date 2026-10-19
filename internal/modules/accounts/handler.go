// Package accounts implements the Account Management module (REQ-4.2-*).
// Every route is administrator-only; the router enforces that.
//
// Creating an account does two things:
//  1. asks Supabase Auth to create the login (email + password), then
//  2. stores the user's profile and role in our users table.
//
// Passwords therefore never touch our code after the request body is read,
// and are never stored by us.
package accounts

import (
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"womads/internal/audit"
	"womads/internal/auth"
	"womads/internal/httpx"
	"womads/internal/models"
	"womads/internal/supabase"
)

type Handler struct {
	DB    *gorm.DB
	Admin *supabase.Admin
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.get)
		r.Patch("/", h.update)
		r.Delete("/", h.remove)
	})
}

const roleHelp = "must be one of: administrator, hr, payroll_officer, applicant, employee"

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r)
	q := h.DB.WithContext(r.Context()).Model(&models.User{})
	if role := r.URL.Query().Get("role"); role != "" {
		if !models.IsValidRole(role) {
			httpx.Invalid(w, httpx.FieldErrors{"role": roleHelp})
			return
		}
		q = q.Where("role = ?", role)
	}
	if s := strings.TrimSpace(r.URL.Query().Get("q")); s != "" {
		like := "%" + httpx.EscapeLike(s) + "%"
		q = q.Where("user_name ILIKE ? OR email ILIKE ?", like, like)
	}
	q = q.Session(&gorm.Session{}) // make q safe to reuse for both queries below

	var total int64
	if err := q.Count(&total).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	users := make([]models.User, 0)
	if err := q.Order("created_at DESC").Limit(p.Size).Offset(p.Offset()).Find(&users).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.List(w, users, p, total)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.IDParam(w, r, "User")
	if !ok {
		return
	}
	u, found, err := h.find(r, id)
	switch {
	case err != nil:
		httpx.Internal(w, r, err)
	case !found:
		httpx.NotFound(w, "User")
	default:
		httpx.Data(w, http.StatusOK, u)
	}
}

type createInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	UserName string `json:"user_name"`
	Role     string `json:"role"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in createInput
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.UserName = strings.TrimSpace(in.UserName)

	v := httpx.NewValidator()
	if a, err := mail.ParseAddress(in.Email); err != nil || a.Address != in.Email {
		v.Add("email", "must be a valid email address")
	}
	if n := len(in.Password); n < 10 || n > 128 {
		v.Add("password", "must be between 10 and 128 characters")
	}
	v.Text("user_name", in.UserName, 100)
	if !models.IsValidRole(in.Role) {
		v.Add("role", roleHelp)
	}
	if !v.OK() {
		httpx.Invalid(w, v.Fields)
		return
	}
	if !h.Admin.Enabled() {
		httpx.Error(w, http.StatusServiceUnavailable, "not_configured",
			"Account creation needs SUPABASE_SERVICE_ROLE_KEY to be set on the server.")
		return
	}

	id, err := h.Admin.CreateUser(r.Context(), supabase.NewUser{
		Email: in.Email, Password: in.Password, FullName: in.UserName, Role: in.Role,
	})
	var rejected *supabase.RejectedError
	switch {
	case errors.Is(err, supabase.ErrEmailTaken):
		httpx.Error(w, http.StatusConflict, "email_taken", "An account with this email already exists.")
		return
	case errors.As(err, &rejected):
		httpx.Error(w, http.StatusUnprocessableEntity, "rejected", rejected.Message)
		return
	case err != nil:
		httpx.Internal(w, r, err)
		return
	}

	// The database trigger normally created the profile already, so upsert.
	u := models.User{ID: id, Role: in.Role, UserName: in.UserName, Email: in.Email, IsActive: true}
	err = h.DB.WithContext(r.Context()).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"role": in.Role, "user_name": in.UserName, "email": in.Email,
			"is_active": true, "deleted_at": nil, "updated_at": time.Now(),
		}),
	}).Create(&u).Error
	if err != nil {
		// Undo the Supabase account so we don't leave a login with no profile.
		if delErr := h.Admin.DeleteUser(r.Context(), id); delErr != nil {
			slog.Error("could not roll back Supabase user", "user_id", id, "err", delErr)
		}
		httpx.Internal(w, r, err)
		return
	}
	audit.Record(r, h.DB, "user.create", "user", id)
	httpx.Data(w, http.StatusCreated, u)
}

type updateInput struct {
	UserName *string `json:"user_name"`
	Role     *string `json:"role"`
	IsActive *bool   `json:"is_active"`
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.IDParam(w, r, "User")
	if !ok {
		return
	}
	var in updateInput
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if in.UserName == nil && in.Role == nil && in.IsActive == nil {
		httpx.Invalid(w, httpx.FieldErrors{"body": "provide at least one of: user_name, role, is_active"})
		return
	}
	v := httpx.NewValidator()
	changes := map[string]any{}
	if in.UserName != nil {
		name := strings.TrimSpace(*in.UserName)
		v.Text("user_name", name, 100)
		changes["user_name"] = name
	}
	if in.Role != nil {
		if !models.IsValidRole(*in.Role) {
			v.Add("role", roleHelp)
		}
		changes["role"] = *in.Role
	}
	if in.IsActive != nil {
		changes["is_active"] = *in.IsActive
	}
	if !v.OK() {
		httpx.Invalid(w, v.Fields)
		return
	}

	current, found, err := h.find(r, id)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if !found {
		httpx.NotFound(w, "User")
		return
	}
	// Stops an administrator from locking themselves out by accident.
	if me := auth.PrincipalFrom(r.Context()); me != nil && me.ID == id {
		if (in.Role != nil && *in.Role != current.Role) || (in.IsActive != nil && !*in.IsActive) {
			httpx.Error(w, http.StatusConflict, "cannot_modify_self",
				"You cannot change your own role or deactivate your own account.")
			return
		}
	}

	if err := h.DB.WithContext(r.Context()).Model(&models.User{}).Where("user_id = ?", id).Updates(changes).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	audit.Record(r, h.DB, "user.update", "user", id)
	updated, _, err := h.find(r, id)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.Data(w, http.StatusOK, updated)
}

// remove archives the profile (soft delete). The row is kept for the audit
// trail, and the account can no longer log in because Authenticate finds no profile.
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.IDParam(w, r, "User")
	if !ok {
		return
	}
	if me := auth.PrincipalFrom(r.Context()); me != nil && me.ID == id {
		httpx.Error(w, http.StatusConflict, "cannot_modify_self", "You cannot delete your own account.")
		return
	}
	res := h.DB.WithContext(r.Context()).Delete(&models.User{}, "user_id = ?", id)
	if res.Error != nil {
		httpx.Internal(w, r, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		httpx.NotFound(w, "User")
		return
	}
	audit.Record(r, h.DB, "user.delete", "user", id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) find(r *http.Request, id string) (models.User, bool, error) {
	var u models.User
	err := h.DB.WithContext(r.Context()).First(&u, "user_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return u, false, nil
	}
	return u, err == nil, err
}
