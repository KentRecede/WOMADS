// Package jobs implements job openings (REQ-4.3-4..6).
//
// Life cycle (Figure 7): HR creates a DRAFT; publishing it is an explicit step
// that checks the listing is complete ("only verified listings are published");
// closing ends it. Applicants only ever see ACTIVE openings, through the
// public routes, and those never reveal which client company the job is for.
package jobs

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"womads/internal/audit"
	"womads/internal/auth"
	"womads/internal/httpx"
	"womads/internal/models"
)

type Handler struct{ DB *gorm.DB }

// Public routes need no login: this is the careers page.
func (h *Handler) Public(r chi.Router) {
	r.Get("/", h.publicList)
	r.Get("/{id}", h.publicGet)
}

// Staff routes are for administrators and HR.
func (h *Handler) Staff(r chi.Router) {
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.get)
		r.Patch("/", h.update)
		r.Delete("/", h.remove)
		r.Post("/publish", h.publish)
		r.Post("/close", h.close)
	})
}

// ---- public ---------------------------------------------------------------

// publicJob is deliberately smaller than models.JobOpening: no client_id and
// no posted_by, so the careers page can't be used to learn the agency's clients.
type publicJob struct {
	ID             string    `gorm:"column:job_id" json:"id"`
	JobTitle       string    `json:"job_title"`
	JobDescription string    `json:"job_description"`
	Location       string    `json:"location"`
	Category       string    `json:"category"`
	PostedAt       time.Time `gorm:"column:created_at" json:"posted_at"`
}

const publicCols = "job_id, job_title, job_description, location, category, created_at"

func (h *Handler) publicList(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r)
	q := h.DB.WithContext(r.Context()).Model(&models.JobOpening{}).Where("status = ?", models.JobActive)
	if s := strings.TrimSpace(r.URL.Query().Get("q")); s != "" {
		like := "%" + httpx.EscapeLike(s) + "%"
		q = q.Where("job_title ILIKE ? OR job_description ILIKE ? OR location ILIKE ?", like, like, like)
	}
	if c := strings.TrimSpace(r.URL.Query().Get("category")); c != "" {
		q = q.Where("LOWER(category) = LOWER(?)", c)
	}
	q = q.Session(&gorm.Session{})

	var total int64
	if err := q.Count(&total).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	rows := make([]publicJob, 0)
	if err := q.Select(publicCols).Order("created_at DESC").Limit(p.Size).Offset(p.Offset()).Find(&rows).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.List(w, rows, p, total)
}

func (h *Handler) publicGet(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.IDParam(w, r, "Job opening")
	if !ok {
		return
	}
	var row publicJob
	res := h.DB.WithContext(r.Context()).Model(&models.JobOpening{}).Select(publicCols).
		Where("job_id = ? AND status = ?", id, models.JobActive).Limit(1).Find(&row)
	switch {
	case res.Error != nil:
		httpx.Internal(w, r, res.Error)
	case res.RowsAffected == 0:
		httpx.NotFound(w, "Job opening")
	default:
		httpx.Data(w, http.StatusOK, row)
	}
}

// ---- staff ----------------------------------------------------------------

type createInput struct {
	ClientID       string  `json:"client_id"`
	JobTitle       string  `json:"job_title"`
	JobDescription *string `json:"job_description"`
	Location       *string `json:"location"`
	Category       *string `json:"category"`
}

type patchInput struct {
	JobTitle       *string `json:"job_title"`
	JobDescription *string `json:"job_description"`
	Location       *string `json:"location"`
	Category       *string `json:"category"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r)
	q := h.DB.WithContext(r.Context()).Model(&models.JobOpening{})
	if s := r.URL.Query().Get("status"); s != "" {
		if s != models.JobDraft && s != models.JobActive && s != models.JobClosed {
			httpx.Invalid(w, httpx.FieldErrors{"status": "must be one of: draft, active, closed"})
			return
		}
		q = q.Where("status = ?", s)
	}
	if c := r.URL.Query().Get("client_id"); c != "" {
		if !httpx.IsUUID(c) {
			httpx.Invalid(w, httpx.FieldErrors{"client_id": "must be a UUID"})
			return
		}
		q = q.Where("client_id = ?", c)
	}
	if s := strings.TrimSpace(r.URL.Query().Get("q")); s != "" {
		q = q.Where("job_title ILIKE ?", "%"+httpx.EscapeLike(s)+"%")
	}
	q = q.Session(&gorm.Session{})

	var total int64
	if err := q.Count(&total).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	items := make([]models.JobOpening, 0)
	if err := q.Order("created_at DESC").Limit(p.Size).Offset(p.Offset()).Find(&items).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.List(w, items, p, total)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.IDParam(w, r, "Job opening")
	if !ok {
		return
	}
	j, found, err := h.find(r, id)
	switch {
	case err != nil:
		httpx.Internal(w, r, err)
	case !found:
		httpx.NotFound(w, "Job opening")
	default:
		httpx.Data(w, http.StatusOK, j)
	}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in createInput
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	v := httpx.NewValidator()
	v.Text("job_title", in.JobTitle, 200)
	checkOptional(v, in.JobDescription, in.Location, in.Category)
	if !httpx.IsUUID(in.ClientID) {
		v.Add("client_id", "is required and must be a UUID")
	} else {
		var n int64
		if err := h.DB.WithContext(r.Context()).Model(&models.Client{}).Where("client_id = ?", in.ClientID).Count(&n).Error; err != nil {
			httpx.Internal(w, r, err)
			return
		}
		if n == 0 {
			v.Add("client_id", "no such client")
		}
	}
	if !v.OK() {
		httpx.Invalid(w, v.Fields)
		return
	}

	j := models.JobOpening{
		ClientID: strings.ToLower(in.ClientID),
		JobTitle: strings.TrimSpace(in.JobTitle),
		Status:   models.JobDraft,
	}
	assign(&j, in.JobDescription, in.Location, in.Category)
	if me := auth.PrincipalFrom(r.Context()); me != nil {
		j.PostedBy = &me.ID
	}
	if err := h.DB.WithContext(r.Context()).Create(&j).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	audit.Record(r, h.DB, "job.create", "job_opening", j.ID)
	httpx.Data(w, http.StatusCreated, j)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.IDParam(w, r, "Job opening")
	if !ok {
		return
	}
	var in patchInput
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	v := httpx.NewValidator()
	if in.JobTitle != nil {
		v.Text("job_title", *in.JobTitle, 200)
	}
	checkOptional(v, in.JobDescription, in.Location, in.Category)
	if !v.OK() {
		httpx.Invalid(w, v.Fields)
		return
	}
	j, found, err := h.find(r, id)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if !found {
		httpx.NotFound(w, "Job opening")
		return
	}
	if j.Status == models.JobClosed {
		httpx.Error(w, http.StatusConflict, "job_closed", "A closed job opening can no longer be edited.")
		return
	}
	if in.JobTitle != nil {
		j.JobTitle = strings.TrimSpace(*in.JobTitle)
	}
	assign(&j, in.JobDescription, in.Location, in.Category)
	if err := h.DB.WithContext(r.Context()).Save(&j).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	audit.Record(r, h.DB, "job.update", "job_opening", j.ID)
	httpx.Data(w, http.StatusOK, j)
}

// publish moves draft -> active after checking the listing is complete.
func (h *Handler) publish(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "job.publish", func(j *models.JobOpening) (*httpx.Validator, string) {
		switch j.Status {
		case models.JobActive:
			return nil, "" // already published: idempotent
		case models.JobClosed:
			return nil, "A closed job opening cannot be republished."
		}
		v := httpx.NewValidator()
		v.Text("job_title", j.JobTitle, 200)
		v.Text("job_description", j.JobDescription, 5000)
		v.Text("location", j.Location, 200)
		v.Text("category", j.Category, 100)
		if !v.OK() {
			return v, ""
		}
		j.Status = models.JobActive
		return nil, ""
	})
}

// close moves active -> closed.
func (h *Handler) close(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "job.close", func(j *models.JobOpening) (*httpx.Validator, string) {
		if j.Status != models.JobActive {
			return nil, "Only active job openings can be closed."
		}
		j.Status = models.JobClosed
		return nil, ""
	})
}

// transition loads a job, lets change adjust it (or refuse), and saves it.
// change returns either field errors (422) or a conflict message (409).
func (h *Handler) transition(w http.ResponseWriter, r *http.Request, action string,
	change func(*models.JobOpening) (*httpx.Validator, string)) {

	id, ok := httpx.IDParam(w, r, "Job opening")
	if !ok {
		return
	}
	j, found, err := h.find(r, id)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if !found {
		httpx.NotFound(w, "Job opening")
		return
	}
	before := j.Status
	v, conflict := change(&j)
	switch {
	case v != nil:
		httpx.Invalid(w, v.Fields)
		return
	case conflict != "":
		httpx.Error(w, http.StatusConflict, "invalid_state", conflict)
		return
	}
	if j.Status != before {
		if err := h.DB.WithContext(r.Context()).Model(&models.JobOpening{}).
			Where("job_id = ?", id).Update("status", j.Status).Error; err != nil {
			httpx.Internal(w, r, err)
			return
		}
		audit.Record(r, h.DB, action, "job_opening", id)
	}
	httpx.Data(w, http.StatusOK, j)
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.IDParam(w, r, "Job opening")
	if !ok {
		return
	}
	j, found, err := h.find(r, id)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if !found {
		httpx.NotFound(w, "Job opening")
		return
	}
	if j.Status == models.JobActive {
		httpx.Error(w, http.StatusConflict, "job_active", "Close this job opening before deleting it.")
		return
	}
	if err := h.DB.WithContext(r.Context()).Delete(&models.JobOpening{}, "job_id = ?", id).Error; err != nil {
		httpx.Internal(w, r, err)
		return
	}
	audit.Record(r, h.DB, "job.delete", "job_opening", id)
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers --------------------------------------------------------------

func checkOptional(v *httpx.Validator, desc, loc, cat *string) {
	for _, f := range []struct {
		name string
		val  *string
		max  int
	}{{"job_description", desc, 5000}, {"location", loc, 200}, {"category", cat, 100}} {
		if f.val != nil && len([]rune(strings.TrimSpace(*f.val))) > f.max {
			v.Add(f.name, "is too long")
		}
	}
}

func assign(j *models.JobOpening, desc, loc, cat *string) {
	if desc != nil {
		j.JobDescription = strings.TrimSpace(*desc)
	}
	if loc != nil {
		j.Location = strings.TrimSpace(*loc)
	}
	if cat != nil {
		j.Category = strings.TrimSpace(*cat)
	}
}

func (h *Handler) find(r *http.Request, id string) (models.JobOpening, bool, error) {
	var j models.JobOpening
	err := h.DB.WithContext(r.Context()).First(&j, "job_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return j, false, nil
	}
	return j, err == nil, err
}
