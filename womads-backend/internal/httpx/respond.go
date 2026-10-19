// Package httpx holds small HTTP helpers shared by every module so all
// endpoints answer in the same JSON shapes.
//
//	success:  {"data": ...}                       lists add {"meta": {page, page_size, total}}
//	failure:  {"error": {"code", "message", "fields"?}}
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

const maxBodyBytes = 1 << 20 // 1 MiB; JSON endpoints never need more

type FieldErrors map[string]string

type errorDetail struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Fields  FieldErrors `json:"fields,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func Data(w http.ResponseWriter, status int, v any) {
	WriteJSON(w, status, map[string]any{"data": v})
}

type Meta struct {
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
}

func List(w http.ResponseWriter, items any, p Page, total int64) {
	WriteJSON(w, http.StatusOK, map[string]any{
		"data": items,
		"meta": Meta{Page: p.Number, PageSize: p.Size, Total: total},
	})
}

func Error(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]any{"error": errorDetail{Code: code, Message: message}})
}

func Invalid(w http.ResponseWriter, fields FieldErrors) {
	WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": errorDetail{
		Code: "validation_error", Message: "One or more fields are invalid.", Fields: fields,
	}})
}

func NotFound(w http.ResponseWriter, what string) {
	Error(w, http.StatusNotFound, "not_found", what+" not found.")
}

// Internal logs the real error server-side and tells the client nothing
// about it. Never put err.Error() in a response: it can leak SQL or secrets.
func Internal(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "err", err, "method", r.Method, "path", r.URL.Path, "request_id", RequestIDFrom(r.Context()))
	Error(w, http.StatusInternalServerError, "internal_error", "Something went wrong. Please try again.")
}

// DecodeJSON reads a JSON body into dst. It rejects unknown fields, trailing
// data and bodies over 1 MiB. On failure it has already written the response
// and returns false.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		switch {
		case errors.As(err, &tooBig):
			Error(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large.")
		case errors.Is(err, io.EOF):
			Error(w, http.StatusBadRequest, "invalid_json", "Request body is empty.")
		default:
			Error(w, http.StatusBadRequest, "invalid_json", "Malformed JSON: "+err.Error())
		}
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		Error(w, http.StatusBadRequest, "invalid_json", "Request body must contain a single JSON object.")
		return false
	}
	return true
}

// ---- pagination -----------------------------------------------------------

type Page struct{ Number, Size int }

func (p Page) Offset() int { return (p.Number - 1) * p.Size }

// ParsePage reads ?page= (default 1) and ?page_size= (default 20, max 100).
func ParsePage(r *http.Request) Page {
	p := Page{Number: 1, Size: 20}
	if n, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && n > 0 {
		p.Number = n
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && n > 0 {
		p.Size = min(n, 100)
	}
	return p
}

// ---- ids and search -------------------------------------------------------

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func IsUUID(s string) bool { return uuidRe.MatchString(s) }

// IDParam returns the {id} URL parameter if it is a UUID; otherwise it writes
// a 404 (a malformed id can't exist) and returns false.
func IDParam(w http.ResponseWriter, r *http.Request, what string) (string, bool) {
	id := chi.URLParam(r, "id")
	if !IsUUID(id) {
		NotFound(w, what)
		return "", false
	}
	return strings.ToLower(id), true
}

// EscapeLike makes user text safe to embed in an ILIKE '%...%' pattern.
func EscapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ---- validation -----------------------------------------------------------

type Validator struct{ Fields FieldErrors }

func NewValidator() *Validator { return &Validator{Fields: FieldErrors{}} }

func (v *Validator) Add(field, msg string) {
	if _, exists := v.Fields[field]; !exists {
		v.Fields[field] = msg
	}
}

// Text checks a required string field is non-blank and at most max runes.
func (v *Validator) Text(field, value string, max int) {
	value = strings.TrimSpace(value)
	switch {
	case value == "":
		v.Add(field, "is required")
	case len([]rune(value)) > max:
		v.Add(field, "must be at most "+strconv.Itoa(max)+" characters")
	}
}

func (v *Validator) OK() bool { return len(v.Fields) == 0 }
