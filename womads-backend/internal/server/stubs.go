package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"womads/internal/auth"
	"womads/internal/httpx"
	"womads/internal/models"
)

// planned lists the routes of the modules that are not built yet.
//
// Each one is already registered with the correct role guard, so the access
// rules from the paper (section 5.5, Business Rules) are in place before any
// logic exists. They answer 501 until you replace them.
//
// To build a module: create internal/modules/<name>/handler.go, mount it in
// router.go, and delete its lines from this table.
var planned = []struct {
	method, path, req string
	roles             []string
}{
	// Online Application — REQ-4.1-*. Only HR (not even admins) approves or rejects (business rule 2).
	{"POST", "/applications", "REQ-4.1-2..4", []string{models.RoleApplicant}},
	{"GET", "/applications/me", "REQ-4.1-5", []string{models.RoleApplicant}},
	{"GET", "/applicants", "REQ-4.1 ranking", hrOrAdmin},
	{"GET", "/applicants/{id}", "REQ-4.1 ranking", hrOrAdmin},
	{"POST", "/applicants/{id}/approve", "business rule 2", []string{models.RoleHR}},
	{"POST", "/applicants/{id}/reject", "business rule 2", []string{models.RoleHR}},

	// Employee Management — REQ-4.3-1..3
	{"GET", "/employees", "REQ-4.3-1", hrOrAdmin},
	{"POST", "/employees", "REQ-4.3-1", hrOrAdmin},
	{"GET", "/employees/{id}", "REQ-4.3-2", hrOrAdmin},
	{"PATCH", "/employees/{id}", "REQ-4.3-2", hrOrAdmin},
	{"DELETE", "/employees/{id}", "REQ-4.3-2", hrOrAdmin},

	// Employee Deployment — REQ-4.5-*
	{"GET", "/deployments", "REQ-4.5-3", hrOrAdmin},
	{"POST", "/deployments", "REQ-4.5-1,2", hrOrAdmin},
	{"POST", "/deployments/{id}/reassign", "REQ-4.5-4", hrOrAdmin},
	{"GET", "/deployments/candidates", "REQ-4.5-5 ranking", hrOrAdmin},

	// Payroll — REQ-4.6-*
	{"POST", "/payroll/imports", "REQ-4.6-1,2", payroll},
	{"POST", "/payroll/compute", "REQ-4.6-3..5", payroll},
	{"GET", "/payroll", "REQ-4.6", payroll},
	{"GET", "/payroll/{id}/payslip.pdf", "REQ-4.6-6", payroll},

	// Reports and Analytics — REQ-4.7-*
	{"GET", "/reports/{type}", "REQ-4.7-1..6", []string{models.RoleAdministrator}},

	// Employee self-service — REQ-4.8-*; employees see only their own data (business rule 7)
	{"GET", "/me/deployment", "REQ-4.8-3", []string{models.RoleEmployee}},
	{"GET", "/me/attendance", "REQ-4.8-4", []string{models.RoleEmployee}},
	{"GET", "/me/payslips", "REQ-4.8-5", []string{models.RoleEmployee}},

	// Notifications — any signed-in user
	{"GET", "/notifications", "REQ-4.8-6", models.Roles},
}

var (
	hrOrAdmin = []string{models.RoleAdministrator, models.RoleHR}
	payroll   = []string{models.RoleAdministrator, models.RolePayrollOfficer}
)

func mountPlanned(r chi.Router) {
	for _, p := range planned {
		r.With(auth.RequireRoles(p.roles...)).MethodFunc(p.method, p.path, notImplemented(p.req))
	}
}

func notImplemented(req string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		httpx.Error(w, http.StatusNotImplemented, "not_implemented", "This module is not built yet ("+req+").")
	}
}
