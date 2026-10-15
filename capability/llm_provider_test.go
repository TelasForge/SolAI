package capability

import (
	"strings"
	"testing"
)

// ---- NewLLMProvider ---------------------------------------------------------

func TestNewLLMProvider_Configured(t *testing.T) {
	p := NewLLMProvider("sk-or-key", "")
	if !p.IsConfigured() {
		t.Error("expected provider to be configured")
	}
	if p.APIKey() != "sk-or-key" {
		t.Errorf("APIKey: got %q", p.APIKey())
	}
}

func TestNewLLMProvider_EmptyKey(t *testing.T) {
	p := NewLLMProvider("", "")
	if p.IsConfigured() {
		t.Error("empty key should not be configured")
	}
}

func TestNewLLMProvider_BaseURL(t *testing.T) {
	p := NewLLMProvider("key", "https://example.test/api/v1")
	if p.BaseURL() != "https://example.test/api/v1" {
		t.Errorf("BaseURL: got %q", p.BaseURL())
	}
}

// ---- ResolveForTool ---------------------------------------------------------

func TestResolveForTool_PrimaryPreferred(t *testing.T) {
	p := NewLLMProvider("key", "")
	opts := []LLMModelOption{
		{Model: "anthropic/claude-sonnet-4"},
		{Model: "openai/gpt-4o"},
	}
	cfg := p.ResolveForTool("anthropic/claude-sonnet-4", opts)
	if cfg == nil {
		t.Fatal("expected non-nil LLMConfig")
	}
	if cfg.Model != "anthropic/claude-sonnet-4" {
		t.Errorf("Model: got %q", cfg.Model)
	}
	if cfg.APIKey != "key" {
		t.Errorf("APIKey: got %q", cfg.APIKey)
	}
}

func TestResolveForTool_FallsBackToFirstSupported(t *testing.T) {
	p := NewLLMProvider("key", "")
	opts := []LLMModelOption{
		{Model: "anthropic/claude-sonnet-4"},
		{Model: "openai/gpt-4o"},
	}
	cfg := p.ResolveForTool("", opts)
	if cfg == nil {
		t.Fatal("expected fallback config")
	}
	if cfg.Model != "anthropic/claude-sonnet-4" {
		t.Errorf("Model: got %q", cfg.Model)
	}
}

func TestResolveForTool_NoAPIKey(t *testing.T) {
	p := NewLLMProvider("", "")
	opts := []LLMModelOption{{Model: "openai/gpt-4o"}}
	if got := p.ResolveForTool("openai/gpt-4o", opts); got != nil {
		t.Errorf("expected nil without an API key, got %+v", got)
	}
}

func TestResolveForTool_EmptySupportedList(t *testing.T) {
	p := NewLLMProvider("key", "")
	if got := p.ResolveForTool("", nil); got != nil {
		t.Errorf("expected nil for empty primary+supported, got %+v", got)
	}
}

// ---- LLMConfig.Env ----------------------------------------------------------

func TestLLMConfigEnv(t *testing.T) {
	cfg := &LLMConfig{
		Model:   "openai/gpt-4o",
		APIKey:  "test-key",
		BaseURL: "https://example.test/api/v1",
	}
	env := cfg.Env()
	want := map[string]string{
		"SOLAI_LLM_MODEL":    "openai/gpt-4o",
		"SOLAI_LLM_API_KEY":  "test-key",
		"SOLAI_LLM_BASE_URL": "https://example.test/api/v1",
	}
	if len(env) != len(want) {
		t.Fatalf("expected %d env vars, got %d: %v", len(want), len(env), env)
	}
	for _, entry := range env {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			t.Errorf("malformed env entry %q", entry)
			continue
		}
		if want[parts[0]] != parts[1] {
			t.Errorf("env %s: got %q, want %q", parts[0], parts[1], want[parts[0]])
		}
	}
}

func TestLLMConfigEnv_OmitsEmptyBaseURL(t *testing.T) {
	cfg := &LLMConfig{Model: "m", APIKey: "k"}
	if env := cfg.Env(); len(env) != 2 {
		t.Errorf("expected 2 env vars without base URL, got %v", env)
	}
}

// ---- ConfiguredProviders ----------------------------------------------------

func TestConfiguredProviders(t *testing.T) {
	p := NewLLMProvider("key", "")
	names := p.ConfiguredProviders()
	if len(names) != 1 || names[0] != "openrouter" {
		t.Errorf("expected [openrouter], got %v", names)
	}
}

func TestConfiguredProviders_Empty(t *testing.T) {
	p := NewLLMProvider("", "")
	if names := p.ConfiguredProviders(); len(names) != 0 {
		t.Errorf("expected empty list, got %v", names)
	}
}

// ---- SupportedModelList -----------------------------------------------------

func TestSupportedModelList_Basic(t *testing.T) {
	opts := []LLMModelOption{
		{Model: "anthropic/claude-sonnet-4"},
		{Model: "openai/gpt-4o"},
	}
	got := SupportedModelList(opts)
	if !strings.Contains(got, "anthropic/claude-sonnet-4") || !strings.Contains(got, "openai/gpt-4o") {
		t.Errorf("unexpected list: %q", got)
	}
}

func TestSupportedModelList_Empty(t *testing.T) {
	if got := SupportedModelList(nil); got != "(none declared)" {
		t.Errorf("expected placeholder, got %q", got)
	}
}
