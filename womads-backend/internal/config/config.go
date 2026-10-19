// Package config reads settings from environment variables.
//
// A local .env file is loaded automatically (if present) so the services run
// the same way on Windows, macOS and Linux without extra tooling. Real
// environment variables always win over the file.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// API holds the settings for the core API service (cmd/api).
type API struct {
	Env         string // "development" or "production"
	Port        string
	DatabaseURL string
	SupabaseURL string // https://<project-ref>.supabase.co
	JWTSecret   string // legacy HS256 secret; optional if your project uses asymmetric signing keys
	ServiceKey  string // Supabase service-role / secret key; optional, needed to create accounts
}

func (a API) IsProduction() bool { return a.Env == "production" }

// Gateway holds the settings for the API gateway (cmd/gateway).
type Gateway struct {
	Env            string
	Port           string
	CoreURL        string
	AllowedOrigins []string
}

func LoadAPI() (API, error) {
	a := API{
		Env:         get("APP_ENV", "development"),
		Port:        get("API_PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		SupabaseURL: strings.TrimRight(os.Getenv("SUPABASE_URL"), "/"),
		JWTSecret:   os.Getenv("SUPABASE_JWT_SECRET"),
		ServiceKey:  os.Getenv("SUPABASE_SERVICE_ROLE_KEY"),
	}
	var missing []string
	if a.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if a.SupabaseURL == "" {
		missing = append(missing, "SUPABASE_URL")
	}
	if len(missing) > 0 {
		return a, fmt.Errorf("missing required environment variables: %s (see .env.example)", strings.Join(missing, ", "))
	}
	if err := checkURL("SUPABASE_URL", a.SupabaseURL); err != nil {
		return a, err
	}
	return a, nil
}

func LoadGateway() (Gateway, error) {
	g := Gateway{
		Env:            get("APP_ENV", "development"),
		Port:           get("GATEWAY_PORT", "8000"),
		CoreURL:        strings.TrimRight(get("CORE_API_URL", "http://localhost:8080"), "/"),
		AllowedOrigins: splitCSV(os.Getenv("CORS_ALLOWED_ORIGINS")),
	}
	if err := checkURL("CORE_API_URL", g.CoreURL); err != nil {
		return g, err
	}
	return g, nil
}

// LoadDotEnv loads KEY=VALUE pairs from path into the process environment.
// Missing files are fine. Variables that are already set are left untouched.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
	return sc.Err()
}

func get(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func checkURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%s must be a full http(s) URL, got %q", name, raw)
	}
	return nil
}
