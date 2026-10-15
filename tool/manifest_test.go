package tool

import (
	"os"
	"path/filepath"
	"testing"
)

// ---- Validate ---------------------------------------------------------------

func TestValidate_Valid(t *testing.T) {
	m := Manifest{
		Name:        "my-tool",
		Description: "does something",
		Version:     "1.0.0",
		Executable:  "./bin/my-tool",
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_MissingName(t *testing.T) {
	m := Manifest{Description: "desc", Executable: "./exec"}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidate_MissingDescription(t *testing.T) {
	m := Manifest{Name: "foo", Executable: "./exec"}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for missing description")
	}
}

func TestValidate_MissingExecutable(t *testing.T) {
	m := Manifest{Name: "foo", Description: "desc"}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for missing executable")
	}
}

func TestValidate_AllFieldsPresent(t *testing.T) {
	m := Manifest{
		Name:        "solana-balance",
		Description: "Queries the Solana blockchain for account balances.",
		Version:     "1.0.0",
		Executable:  "./bin/solana-balance",
	}
	if err := m.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---- LoadManifest -----------------------------------------------------------

func writeManifestFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadManifest_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	content := `{
		"name": "token-price",
		"description": "Fetches USD prices.",
		"version": "1.0.0",
		"executable": "./bin/token-price",
		"required_capabilities": ["network-manager"]
	}`
	path := writeManifestFile(t, dir, content)

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Name != "token-price" {
		t.Errorf("Name: got %q, want %q", m.Name, "token-price")
	}
	if m.Executable != "./bin/token-price" {
		t.Errorf("Executable: got %q", m.Executable)
	}
	if len(m.RequiredCapabilities) != 1 || m.RequiredCapabilities[0] != "network-manager" {
		t.Errorf("RequiredCapabilities: got %v", m.RequiredCapabilities)
	}
}

func TestLoadManifest_WithTimeout(t *testing.T) {
	dir := t.TempDir()
	content := `{"name":"t","description":"d","executable":"./e","timeout":"90s"}`
	path := writeManifestFile(t, dir, content)

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Timeout != "90s" {
		t.Errorf("Timeout: got %q, want %q", m.Timeout, "90s")
	}
}

func TestLoadManifest_WithLLMOptions(t *testing.T) {
	dir := t.TempDir()
	content := `{
		"name": "my-tool",
		"description": "desc",
		"executable": "./bin/my-tool",
		"llm_options": {
			"primary": "gemini-2.5-pro",
			"supported": [
				{"model": "gemini-2.5-pro", "provider": "google"},
				{"model": "gpt-4o", "provider": "openai"}
			]
		}
	}`
	path := writeManifestFile(t, dir, content)

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.LLMOptions == nil {
		t.Fatal("expected LLMOptions to be set")
	}
	if m.LLMOptions.Primary != "gemini-2.5-pro" {
		t.Errorf("Primary: got %q", m.LLMOptions.Primary)
	}
	if len(m.LLMOptions.Supported) != 2 {
		t.Errorf("Supported: expected 2, got %d", len(m.LLMOptions.Supported))
	}
}

func TestLoadManifest_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeManifestFile(t, dir, "{not valid json")

	if _, err := LoadManifest(path); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadManifest_FileNotFound(t *testing.T) {
	if _, err := LoadManifest("/nonexistent/path/manifest.json"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadManifest_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeManifestFile(t, dir, "")
	// Empty file is invalid JSON.
	if _, err := LoadManifest(path); err == nil {
		t.Fatal("expected error for empty file")
	}
}
