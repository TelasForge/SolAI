package tool

import (
	"encoding/json"
	"fmt"
	"os"
)

// ManifestLLMModel is one entry in a tool's supported model list.
type ManifestLLMModel struct {
	// Model is an OpenRouter model slug, e.g. "anthropic/claude-sonnet-4" or
	// "openai/gpt-4o".
	Model string `json:"model"`
}

// LLMOptions declares which LLM models a tool supports.
// If absent from manifest.json, the tool does not require an LLM and loads
// regardless of which providers are configured.
type LLMOptions struct {
	// Primary is the model name the tool prefers. The LLM provider will try
	// to supply this model's credentials first.
	Primary string `json:"primary"`

	// Supported lists all models this tool can work with, in preference order.
	// The first model whose provider is configured (after checking Primary) is used.
	Supported []ManifestLLMModel `json:"supported"`
}

// EnvVar declares a single environment variable a tool requires at runtime.
type EnvVar struct {
	// Name is the environment variable name, e.g. "BIRDEYE_API_KEY".
	Name string `json:"name"`

	// Sensitive marks the value as secret — it will be redacted in config list output.
	Sensitive bool `json:"sensitive"`

	// Required causes the agent to refuse to load the tool if the value is not set.
	Required bool `json:"required"`
}

// ManifestPayload declares a named payload entry a tool uses.
type ManifestPayload struct {
	// Name is the payload key (e.g. "wallet_address").
	Name string `json:"name"`

	// Description is shown in the tool's Description() so the coordinator LLM
	// knows what the payload contains.
	Description string `json:"description"`

	// Source is a capability name whose Execute() result is auto-injected by
	// the coordinator at load time (e.g. "wallet"). Empty means the value is
	// provided via capability request fulfillment on re-invocation.
	Source string `json:"source,omitempty"`
}

// Manifest represents the contents of a tool's manifest.json file.
// Every agentic tool directory must contain one.
type Manifest struct {
	// Name is the tool's unique identifier used by the LLM to invoke it.
	// Convention: lowercase with hyphens, e.g. "solana-balance".
	Name string `json:"name"`

	// Description is shown verbatim to the LLM to help it decide when to
	// use this tool. Should describe what the tool does and what input it expects.
	Description string `json:"description"`

	// Version is the semver string for the tool, e.g. "1.0.0".
	Version string `json:"version"`

	// Executable is the path to the binary or script, relative to the manifest
	// directory. E.g. "./solana-balance" or "./run.sh".
	Executable string `json:"executable"`

	// LLMOptions declares LLM model preferences for tools that need their own LLM.
	// Omit this field for tools that do not require an LLM.
	LLMOptions *LLMOptions `json:"llm_options,omitempty"`

	// RequiredCapabilities lists Regular capabilities this tool needs.
	// Each entry is a capability name (e.g. "network-manager", "file-manager").
	// Tools declaring capabilities that are not registered are disabled at load
	// time with a warning. An absent or empty list means no extra capabilities.
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`

	// Timeout is a Go duration string (e.g. "90s", "2m") for how long a single
	// tool invocation may run before it is killed. Defaults to DefaultToolTimeout
	// when absent or unparseable.
	Timeout string `json:"timeout,omitempty"`

	// Payloads declares named payload values the tool accepts. Entries with a
	// Source field are auto-injected by the coordinator; others are provided
	// by the coordinator LLM in response to capability requests.
	Payloads []ManifestPayload `json:"payloads,omitempty"`

	// Env declares environment variables the tool needs at runtime.
	// Values are sourced from the agent config (tool-env.<name>.<VAR>).
	Env []EnvVar `json:"env,omitempty"`
}

// LoadManifest reads and JSON-decodes the manifest.json at the given file path.
func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("reading manifest %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parsing manifest %s: %w", path, err)
	}
	return m, nil
}

// Validate checks that required Manifest fields are non-empty.
func (m Manifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("manifest missing required field: name")
	}
	if m.Description == "" {
		return fmt.Errorf("manifest %q missing required field: description", m.Name)
	}
	if m.Version == "" {
		return fmt.Errorf("manifest %q missing required field: version", m.Name)
	}
	if m.Executable == "" {
		return fmt.Errorf("manifest %q missing required field: executable", m.Name)
	}
	return nil
}
