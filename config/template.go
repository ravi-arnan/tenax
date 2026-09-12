package config

import (
	"fmt"
	"os"
	"strings"
	"text/template"
)

// HostVars holds per-host variables that can be used in service templates.
// The agent loads these from /etc/tenax/vars.yaml or TENAX_VARS.
type HostVars struct {
	Variables map[string]string `yaml:"vars"`
}

// DefaultVarsPath returns the default path for the host variables file.
func DefaultVarsPath() string {
	if p := os.Getenv("TENAX_VARS"); p != "" {
		return p
	}
	return "/etc/tenax/vars.yaml"
}

// Merge combines two HostVars, with overrides winning on conflict.
func (hv *HostVars) Merge(overrides *HostVars) *HostVars {
	merged := &HostVars{Variables: make(map[string]string)}
	if hv != nil {
		for k, v := range hv.Variables {
			merged.Variables[k] = v
		}
	}
	if overrides != nil {
		for k, v := range overrides.Variables {
			merged.Variables[k] = v
		}
	}
	return merged
}

// TemplateContext provides the data available inside service templates.
type TemplateContext struct {
	// Service is the name of the service being rendered.
	Service string

	// Vars holds all resolved variables (env + secrets + host vars).
	Vars map[string]string
}

// RenderTemplate evaluates a Go text/template string with the given context.
// It returns the rendered string or an error.
func RenderTemplate(tmplStr string, ctx *TemplateContext) (string, error) {
	if ctx == nil {
		ctx = &TemplateContext{Vars: make(map[string]string)}
	}
	if ctx.Vars == nil {
		ctx.Vars = make(map[string]string)
	}

	t, err := template.New("service").Funcs(templateFuncs).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var b strings.Builder
	if err := t.Execute(&b, ctx); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return b.String(), nil
}

// templateFuncs available inside service templates.
var templateFuncs = template.FuncMap{
	"default": func(def, val interface{}) string {
		if val == nil {
			return def.(string)
		}
		if s, ok := val.(string); ok {
			if s == "" {
				return def.(string)
			}
			return s
		}
		return def.(string)
	},
	"env": func(key string) string {
		return os.Getenv(key)
	},
	"upper": strings.ToUpper,
	"lower": strings.ToLower,
	"trim":  strings.TrimSpace,
}
