package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// SecretsSource resolves secret values by key name. Implementations include
// env-file loading and direct environment variable lookup.
type SecretsSource interface {
	Get(key string) (string, bool)
}

// SecretsEnvFile loads secrets from a simple KEY=VALUE file (one per line).
// Lines starting with # and blank lines are skipped. No quoting required
// for values without spaces; wrap in single or double quotes to include
// spaces.
type SecretsEnvFile struct {
	values map[string]string
}

// LoadSecretsFile parses a .env-style secrets file.
func LoadSecretsFile(path string) (*SecretsEnvFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open secrets %s: %w", path, err)
	}
	defer f.Close()

	sf := &SecretsEnvFile{values: make(map[string]string)}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// Strip surrounding quotes.
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		sf.values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read secrets: %w", err)
	}
	return sf, nil
}

func (sf *SecretsEnvFile) Get(key string) (string, bool) {
	v, ok := sf.values[key]
	return v, ok
}

// SecretsEnv looks up keys from the process environment.
type SecretsEnv struct{}

func (se SecretsEnv) Get(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	return v, ok
}

// SecretsChain tries sources in order; first match wins.
type SecretsChain struct {
	sources []SecretsSource
}

// NewSecretsChain creates a chain that tries sources in order.
func NewSecretsChain(sources ...SecretsSource) *SecretsChain {
	return &SecretsChain{sources: sources}
}

func (sc *SecretsChain) Get(key string) (string, bool) {
	for _, src := range sc.sources {
		if v, ok := src.Get(key); ok {
			return v, true
		}
	}
	return "", false
}

// DefaultSecretsPath returns the default path for the secrets .env file.
func DefaultSecretsPath() string {
	if p := os.Getenv("TENAX_SECRETS"); p != "" {
		return p
	}
	return "/etc/tenax/secrets.env"
}

// ResolveEnvVars resolves $secret:NAME references in an env map.
// For each value in env, if it matches "$secret:KEY", it is replaced with
// the resolved secret. If the secret is not found, an error is returned.
func ResolveEnvVars(env map[string]string, src SecretsSource) (map[string]string, error) {
	if len(env) == 0 {
		return env, nil
	}
	resolved := make(map[string]string, len(env))
	for k, v := range env {
		if strings.HasPrefix(v, "$secret:") {
			key := strings.TrimPrefix(v, "$secret:")
			val, ok := src.Get(key)
			if !ok {
				return nil, fmt.Errorf("secret %q not found", key)
			}
			resolved[k] = val
		} else {
			resolved[k] = v
		}
	}
	return resolved, nil
}
