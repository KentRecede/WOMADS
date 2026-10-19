package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	content := "# comment\n\nA_KEY=plain\nexport B_KEY=\"quoted value\"\nC_KEY='single'\nD_KEY=has=equals\nEXISTING=fromfile\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXISTING", "fromenv")
	for _, k := range []string{"A_KEY", "B_KEY", "C_KEY", "D_KEY"} {
		os.Unsetenv(k)
	}
	if err := LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A_KEY": "plain", "B_KEY": "quoted value", "C_KEY": "single", "D_KEY": "has=equals", "EXISTING": "fromenv"}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if err := LoadDotEnv(filepath.Join(dir, "nope.env")); err != nil {
		t.Errorf("missing file should not error: %v", err)
	}
}

func TestLoadAPIValidation(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SUPABASE_URL", "")
	if _, err := LoadAPI(); err == nil {
		t.Fatal("expected error when required vars are missing")
	}
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("SUPABASE_URL", "not-a-url")
	if _, err := LoadAPI(); err == nil {
		t.Fatal("expected error for bad SUPABASE_URL")
	}
	t.Setenv("SUPABASE_URL", "https://abc.supabase.co/")
	a, err := LoadAPI()
	if err != nil {
		t.Fatal(err)
	}
	if a.SupabaseURL != "https://abc.supabase.co" || a.Port != "8080" {
		t.Errorf("unexpected config: %+v", a)
	}
}
