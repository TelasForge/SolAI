package capability

import (
	"strings"
)

// LLMModelOption is one model entry declared by a tool's manifest.
type LLMModelOption struct {
	// Model is an OpenRouter model slug, e.g. "anthropic/claude-sonnet-4".
	Model string
}

// LLMConfig is the resolved model and credentials injected into a tool subprocess.
type LLMConfig struct {
	Model   string
	APIKey  string
	BaseURL string
}

// Env returns env var assignments for the tool subprocess. Tools build their
// own OpenRouter client from SOLAI_LLM_MODEL, SOLAI_LLM_API_KEY, and
// (optionally) SOLAI_LLM_BASE_URL.
func (c *LLMConfig) Env() []string {
	env := []string{
		"SOLAI_LLM_MODEL=" + c.Model,
		"SOLAI_LLM_API_KEY=" + c.APIKey,
	}
	if c.BaseURL != "" {
		env = append(env, "SOLAI_LLM_BASE_URL="+c.BaseURL)
	}
	return env
}

// LLMProvider stores the OpenRouter credentials shared by the coordinator and
// every agentic tool. It is a Core-class system component: invisible to the
// main LLM and agentic tools.
//
// OpenRouter unifies access to Google/OpenAI/Anthropic/… models behind a
// single key, so there is only one credential to manage.
type LLMProvider struct {
	apiKey  string
	baseURL string
}

// NewLLMProvider builds a provider from an OpenRouter API key and optional
// base URL. An empty base URL falls back to the OpenRouter default.
func NewLLMProvider(apiKey, baseURL string) *LLMProvider {
	return &LLMProvider{apiKey: apiKey, baseURL: baseURL}
}

// IsConfigured reports whether an OpenRouter API key is present.
func (p *LLMProvider) IsConfigured() bool { return p.apiKey != "" }

// ResolveForTool selects the model to inject into a tool. With a single
// OpenRouter credential, the primary model is always usable; when it is empty
// the first supported model is used instead.
//
// Returns nil when no API key is configured, in which case the caller should
// disable any tool that requires an LLM.
func (p *LLMProvider) ResolveForTool(primary string, supported []LLMModelOption) *LLMConfig {
	if p.apiKey == "" {
		return nil
	}
	model := primary
	if model == "" && len(supported) > 0 {
		model = supported[0].Model
	}
	if model == "" {
		return nil
	}
	return &LLMConfig{
		Model:   model,
		APIKey:  p.apiKey,
		BaseURL: p.baseURL,
	}
}

// ConfiguredProviders returns the configured provider names, for startup logging.
func (p *LLMProvider) ConfiguredProviders() []string {
	if p.apiKey == "" {
		return nil
	}
	return []string{"openrouter"}
}

// SupportedModelList formats a list of model options as a readable string,
// used in warning messages when a tool is disabled.
func SupportedModelList(models []LLMModelOption) string {
	parts := make([]string, 0, len(models))
	for _, m := range models {
		if m.Model != "" {
			parts = append(parts, m.Model)
		}
	}
	if len(parts) == 0 {
		return "(none declared)"
	}
	return strings.Join(parts, ", ")
}

// APIKey returns the configured OpenRouter key (used by the coordinator to
// build its own client).
func (p *LLMProvider) APIKey() string { return p.apiKey }

// BaseURL returns the configured base URL (may be empty).
func (p *LLMProvider) BaseURL() string { return p.baseURL }
