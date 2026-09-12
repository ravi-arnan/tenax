package config_test

import (
	"testing"

	"github.com/ravi-arnan/tenax/config"
)

func TestRenderTemplate(t *testing.T) {
	tmpl := `server {
    listen {{.Vars.port}};
    server_name {{.Vars.server_name}};
}`
	ctx := &config.TemplateContext{
		Service: "nginx",
		Vars: map[string]string{
			"port":        "80",
			"server_name": "example.com",
		},
	}

	got, err := config.RenderTemplate(tmpl, ctx)
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}

	if !contains(got, "listen 80;") {
		t.Errorf("missing listen directive: %s", got)
	}
	if !contains(got, "server_name example.com;") {
		t.Errorf("missing server_name: %s", got)
	}
}

func TestRenderTemplateWithDefault(t *testing.T) {
	tmpl := `port={{ default "8080" .Vars.port }}`
	ctx := &config.TemplateContext{
		Service: "app",
		Vars:    map[string]string{},
	}

	got, err := config.RenderTemplate(tmpl, ctx)
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "port=8080" {
		t.Errorf("expected port=8080, got %q", got)
	}
}

func TestRenderTemplateWithUpper(t *testing.T) {
	tmpl := `ENV={{ upper .Vars.env }}`
	ctx := &config.TemplateContext{
		Service: "app",
		Vars:    map[string]string{"env": "production"},
	}

	got, err := config.RenderTemplate(tmpl, ctx)
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "ENV=PRODUCTION" {
		t.Errorf("expected ENV=PRODUCTION, got %q", got)
	}
}

func TestRenderTemplateBadSyntax(t *testing.T) {
	_, err := config.RenderTemplate("{{invalid}}", &config.TemplateContext{})
	if err == nil {
		t.Fatal("expected error for bad template syntax")
	}
}

func TestHostVarsMerge(t *testing.T) {
	base := &config.HostVars{Variables: map[string]string{
		"a": "1",
		"b": "2",
	}}
	override := &config.HostVars{Variables: map[string]string{
		"b": "override",
		"c": "3",
	}}

	merged := base.Merge(override)
	if merged.Variables["a"] != "1" {
		t.Errorf("a = %q", merged.Variables["a"])
	}
	if merged.Variables["b"] != "override" {
		t.Errorf("b = %q, want override", merged.Variables["b"])
	}
	if merged.Variables["c"] != "3" {
		t.Errorf("c = %q", merged.Variables["c"])
	}
}

func TestHostVarsMergeNil(t *testing.T) {
	base := &config.HostVars{Variables: map[string]string{"a": "1"}}
	merged := base.Merge(nil)
	if merged.Variables["a"] != "1" {
		t.Errorf("a = %q", merged.Variables["a"])
	}

	merged2 := (*config.HostVars)(nil).Merge(base)
	if merged2.Variables["a"] != "1" {
		t.Errorf("a = %q (nil merge)", merged2.Variables["a"])
	}
}

func TestRenderTemplateNilVars(t *testing.T) {
	got, err := config.RenderTemplate("hello", &config.TemplateContext{})
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "hello" {
		t.Errorf("expected hello, got %q", got)
	}

	got, err = config.RenderTemplate("hello world", nil)
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "hello world" {
		t.Errorf("expected hello world, got %q", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
