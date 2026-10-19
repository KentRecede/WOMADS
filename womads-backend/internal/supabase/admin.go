// Package supabase is a tiny client for the one Supabase Auth admin feature
// the backend needs: creating (and, if something goes wrong, deleting) user
// accounts. It needs the service-role / secret key, which must never leave
// the server.
package supabase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Admin struct {
	baseURL string
	key     string
	client  *http.Client
}

func NewAdmin(supabaseURL, serviceKey string) *Admin {
	return &Admin{
		baseURL: strings.TrimRight(supabaseURL, "/"),
		key:     serviceKey,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Enabled reports whether a service key was configured.
func (a *Admin) Enabled() bool { return a != nil && a.key != "" }

type NewUser struct {
	Email    string
	Password string
	FullName string
	Role     string // stored in app_metadata, which only this key can write
}

var ErrEmailTaken = errors.New("email already registered")

// RejectedError means Supabase refused the request (for example a weak password).
type RejectedError struct{ Message string }

func (e *RejectedError) Error() string { return e.Message }

// CreateUser creates a confirmed account and returns its id.
// Our database trigger creates the matching row in public.users.
func (a *Admin) CreateUser(ctx context.Context, u NewUser) (string, error) {
	payload, _ := json.Marshal(map[string]any{
		"email":         u.Email,
		"password":      u.Password,
		"email_confirm": true,
		"app_metadata":  map[string]any{"role": u.Role},
		"user_metadata": map[string]any{"full_name": u.FullName},
	})
	status, body, err := a.do(ctx, http.MethodPost, "/auth/v1/admin/users", payload)
	if err != nil {
		return "", err
	}
	switch {
	case status == http.StatusOK || status == http.StatusCreated:
		var out struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(body, &out); err != nil || out.ID == "" {
			return "", errors.New("supabase: unexpected response when creating user")
		}
		return out.ID, nil
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		var e struct {
			Code string `json:"error_code"`
			Msg  string `json:"msg"`
		}
		_ = json.Unmarshal(body, &e)
		if e.Code == "email_exists" || e.Code == "user_already_exists" {
			return "", ErrEmailTaken
		}
		if e.Msg == "" {
			e.Msg = "The account could not be created."
		}
		return "", &RejectedError{Message: e.Msg}
	default:
		return "", fmt.Errorf("supabase: create user returned status %d", status)
	}
}

// DeleteUser removes an account (used to undo CreateUser if our own insert fails).
func (a *Admin) DeleteUser(ctx context.Context, id string) error {
	status, _, err := a.do(ctx, http.MethodDelete, "/auth/v1/admin/users/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("supabase: delete user returned status %d", status)
	}
	return nil
}

func (a *Admin) do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", a.key)
	// Legacy service_role keys are JWTs and are also sent as a bearer token.
	// The newer sb_secret_... keys are not JWTs and go in the apikey header only.
	if strings.HasPrefix(a.key, "eyJ") {
		req.Header.Set("Authorization", "Bearer "+a.key)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("supabase request failed: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b, nil
}
