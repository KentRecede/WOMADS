// Package clients implements the Client Management module (REQ-4.4-*).
// Routes are available to administrators and HR staff.
package clients

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"womads/internal/audit"
	"womads/internal/httpx"
	"womads/internal/models"
)

type Handler struct{ DB *gorm.DB }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.get)
		r.Patch("/", h.update)
		r.Delete("/", h.remove)
		r.Get("/deployments", h.deployments)
	})
}

// input is used for both create and update. Pointers distinguish "field not
// sent" (nil) from "field sent as empty/zero", which is what makes PATCH work.
type input struct {
	CompanyName     *string      `json:"company_name"`
	ContactPerson   *string      `json:"contact_person"`
	NumberOfWorkers *int         `json:"number_of_workers"`
	StartDate       *models.Date `json:"start_date"`
	EndDate         *models.Date `json:"end_date"`
}

func (in input) applyTo(c *models.Client) {
	if in.CompanyName != nil {
		c.CompanyName = strings.TrimSpace(*in.CompanyName)
	}
	if in.ContactPerson != nil {
		c.ContactPerson = strings.TrimSpace(*in.ContactPerson)
	}
	if in.NumberOfWorkers != nil {
		c.NumberOfWorkers = *in.NumberOfWorkers
	}
	if in.StartDate != nil {
		c.StartDate = *in.StartDate
	}
	if in.EndDate != nil {
		c.EndDate = *in.EndDate
	}
}

// validate is the "validation gateway" from the paper's Figure 8: contract
// parameters must be complete before a client is stored, because the worker
// quota drives the deployment rules later.
func validate(c *models.Client) *httpx.Validator {
	v := httpx.NewValidator()
	v.Text("company_name", c.CompanyName, 200)
	v.Text("contact_person", c.ContactPerson, 200)
	if c.NumberOfWorkers < 1 {
		v.Add("number_of_workers", "must be at least 1")
	}
	if c.StartDate.IsZero() {
		v.Add("start_date", "is required (YYYY-MM-DD)")
	}
	if c.EndDate.IsZero() {
		v.Add("end_date", "is required (YYYY-MM-DD)")
	}
	if !c.StartDate.IsZero() && !c.EndDate.IsZero() && c.EndDate.Before(c.StartDate.Time) {
		v.Add("end_date", "must be on or after start_date")
	}
	return v
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r)
	q := h.DB.WithContext(r.Context()).Model(&models.Client{})
	if s := strings.TrimSpace(r.URL.Query().Get("q")); s != "" {
		q = q.Where("company_name ILIKE ?", "%"+httpx.EscapeLike(s)+"%")
	}
	q = q.Session(&gorm.Session{})

	var total int64
	if err := q.Count(&total).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	items := make([]models.Client, 0)
	if err := q.Order("company_name ASC").Limit(p.Size).Offset(p.Offset()).Find(&items).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.List(w, items, p, total)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.IDParam(w, r, "Client")
	if !ok {
		return
	}
	c, found, err := h.find(r, id)
	switch {
	case err != nil:
		httpx.Internal(w, r, err)
	case !found:
		httpx.NotFound(w, "Client")
	default:
		httpx.Data(w, http.StatusOK, c)
	}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in input
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	var c models.Client
	in.applyTo(&c)
	if v := validate(&c); !v.OK() {
		httpx.Invalid(w, v.Fields)
		return
	}
	if err := h.DB.WithContext(r.Context()).Create(&c).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	audit.Record(r, h.DB, "client.create", "client", c.ID)
	httpx.Data(w, http.StatusCreated, c)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.IDParam(w, r, "Client")
	if !ok {
		return
	}
	var in input
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	c, found, err := h.find(r, id)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if !found {
		httpx.NotFound(w, "Client")
		return
	}
	in.applyTo(&c)
	if v := validate(&c); !v.OK() { // validate the merged result, not just the patch
		httpx.Invalid(w, v.Fields)
		return
	}
	if err := h.DB.WithContext(r.Context()).Save(&c).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	audit.Record(r, h.DB, "client.update", "client", c.ID)
	httpx.Data(w, http.StatusOK, c)
}

// remove archives a client (soft delete), unless employees are still deployed there.
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.IDParam(w, r, "Client")
	if !ok {
		return
	}
	var active int64
	if err := h.DB.WithContext(r.Context()).Model(&models.Deployment{}).
		Where("client_id = ? AND status = ?", id, models.DeploymentActive).Count(&active).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if active > 0 {
		httpx.Error(w, http.StatusConflict, "has_active_deployments",
			"This client still has deployed employees. Reassign or end those deployments first.")
		return
	}
	res := h.DB.WithContext(r.Context()).Delete(&models.Client{}, "client_id = ?", id)
	if res.Error != nil {
		httpx.Internal(w, r, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		httpx.NotFound(w, "Client")
		return
	}
	audit.Record(r, h.DB, "client.delete", "client", id)
	w.WriteHeader(http.StatusNoContent)
}

type deploymentRow struct {
	DeploymentID string      `json:"deployment_id"`
	EmployeeID   string      `json:"employee_id"`
	EmployeeName string      `json:"employee_name"`
	Position     string      `json:"position"`
	StartDate    models.Date `json:"start_date"`
	Status       string      `json:"status"`
}

// deployments is the "View" function from Figure 8: employees currently
// deployed to this client, read from the DEPLOYMENTS table, with the contract
// quota so the UI can show "12 of 20 workers".
func (h *Handler) deployments(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.IDParam(w, r, "Client")
	if !ok {
		return
	}
	c, found, err := h.find(r, id)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if !found {
		httpx.NotFound(w, "Client")
		return
	}
	rows := make([]deploymentRow, 0)
	err = h.DB.WithContext(r.Context()).Table("deployments d").
		Select("d.deployment_id, d.employee_id, e.name AS employee_name, e.position, d.start_date, d.status").
		Joins("JOIN employees e ON e.employee_id = d.employee_id AND e.deleted_at IS NULL").
		Where("d.client_id = ? AND d.status = ?", id, models.DeploymentActive).
		Order("e.name ASC").Scan(&rows).Error
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"data": rows,
		"meta": map[string]any{"quota": c.NumberOfWorkers, "deployed": len(rows)},
	})
}

func (h *Handler) find(r *http.Request, id string) (models.Client, bool, error) {
	var c models.Client
	err := h.DB.WithContext(r.Context()).First(&c, "client_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c, false, nil
	}
	return c, err == nil, err
}
