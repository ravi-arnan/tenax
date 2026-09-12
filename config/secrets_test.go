package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ravi-arnan/tenax/config"
)

func TestLoadSecretsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	content := `# Database credentials
DB_PASSWORD=s3cret
DB_HOST=localhost

# Quoted value with spaces
API_KEY = "my secret api key"

# Single-quoted
TOKEN='my-token'
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	sf, err := config.LoadSecretsFile(path)
	if err != nil {
		t.Fatalf("LoadSecretsFile: %v", err)
	}

	tests := []struct {
		key   string
		want  string
		found bool
	}{
		{"DB_PASSWORD", "s3cret", true},
		{"DB_HOST", "localhost", true},
		{"API_KEY", "my secret api key", true},
		{"TOKEN", "my-token", true},
		{"MISSING", "", false},
	}
	for _, tc := range tests {
		got, ok := sf.Get(tc.key)
		if ok != tc.found {
			t.Errorf("Get(%q) found = %v, want %v", tc.key, ok, tc.found)
		}
		if ok && got != tc.want {
			t.Errorf("Get(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestSecretsEnv(t *testing.T) {
	t.Setenv("TEST_SECRET_VAR", "from-env")
	src := config.SecretsEnv{}

	val, ok := src.Get("TEST_SECRET_VAR")
	if !ok || val != "from-env" {
		t.Errorf("SecretsEnv.Get = (%q, %v), want (from-env, true)", val, ok)
	}

	_, ok = src.Get("NONEXISTENT")
	if ok {
		t.Error("expected not found")
	}
}

func TestSecretsChain(t *testing.T) {
	chain := config.NewSecretsChain(
		config.SecretsEnv{},
		&config.SecretsEnvFile{}, // empty file source
	)

	// From env.
	t.Setenv("CHAIN_TEST", "chain-val")
	val, ok := chain.Get("CHAIN_TEST")
	if !ok || val != "chain-val" {
		t.Errorf("chain from env: (%q, %v)", val, ok)
	}
}

func TestResolveEnvVars(t *testing.T) {
	env := map[string]string{
		"DB_HOST":     "localhost",
		"DB_PASSWORD": "$secret:DB_PASS",
		"API_KEY":     "$secret:API_KEY",
	}

	// Create a secrets source.
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(path, []byte("DB_PASS=supersecret\nAPI_KEY=abc123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sf, err := config.LoadSecretsFile(path)
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := config.ResolveEnvVars(env, sf)
	if err != nil {
		t.Fatalf("ResolveEnvVars: %v", err)
	}

	if resolved["DB_HOST"] != "localhost" {
		t.Errorf("DB_HOST = %q", resolved["DB_HOST"])
	}
	if resolved["DB_PASSWORD"] != "supersecret" {
		t.Errorf("DB_PASSWORD = %q, want supersecret", resolved["DB_PASSWORD"])
	}
	if resolved["API_KEY"] != "abc123" {
		t.Errorf("API_KEY = %q, want abc123", resolved["API_KEY"])
	}
}

func TestResolveEnvVarsMissingSecret(t *testing.T) {
	env := map[string]string{
		"BAD": "$secret:DOES_NOT_EXIST",
	}
	chain := config.NewSecretsChain(config.SecretsEnv{})

	_, err := config.ResolveEnvVars(env, chain)
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestResolveEnvVarsEmpty(t *testing.T) {
	resolved, err := config.ResolveEnvVars(nil, config.SecretsEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved != nil {
		t.Errorf("expected nil, got %v", resolved)
	}
}
