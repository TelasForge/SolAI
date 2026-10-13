package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useTempHome redirects ~/.solai to a temp directory for the duration of the test.
func useTempHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return tmp
}

// ---- DefaultConfig --------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.CycleTimeout != "5m" {
		t.Errorf("CycleTimeout: got %q, want %q", cfg.CycleTimeout, "5m")
	}
	if cfg.Providers == nil {
		t.Error("Providers: expected non-nil map")
	}
	if cfg.Model.Name != "" {
		t.Error("Model.Name: expected empty by default")
	}
}

// ---- Set ------------------------------------------------------------------

func TestSet_ModelName(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Set("model.name", "anthropic/claude-sonnet-4"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Model.Name != "anthropic/claude-sonnet-4" {
		t.Errorf("got %q", cfg.Model.Name)
	}
}

func TestSet_Providers(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Set("provider.openrouter", "sk-or-key"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Providers["openrouter"] != "sk-or-key" {
		t.Errorf("Providers[openrouter] = %q", cfg.Providers["openrouter"])
	}
}

func TestSet_OpenRouterBaseURL(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Set("openrouter.base-url", "https://example.test/api/v1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OpenRouterBaseURL != "https://example.test/api/v1" {
		t.Errorf("got %q", cfg.OpenRouterBaseURL)
	}
}

func TestSet_WalletSeed(t *testing.T) {
	cfg := DefaultConfig()
	_ = cfg.Set("wallet-seed", "word1 word2")
	if cfg.WalletSeed != "word1 word2" {
		t.Errorf("got %q", cfg.WalletSeed)
	}
}

func TestSet_CycleTimeout(t *testing.T) {
	cfg := DefaultConfig()
	_ = cfg.Set("cycle-timeout", "10m")
	if cfg.CycleTimeout != "10m" {
		t.Errorf("got %q", cfg.CycleTimeout)
	}
}

func TestSet_UserGoals(t *testing.T) {
	cfg := DefaultConfig()
	_ = cfg.Set("user-goals", "monitor price")
	if cfg.UserGoals != "monitor price" {
		t.Errorf("got %q", cfg.UserGoals)
	}
}

func TestSet_SolanaCommitment(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Set("solana.commitment", "finalized"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Solana.Commitment != "finalized" {
		t.Errorf("got %q", cfg.Solana.Commitment)
	}
	if err := cfg.Set("solana.commitment", "bogus"); err == nil {
		t.Error("expected error for invalid commitment")
	}
}

func TestSet_UnknownKey(t *testing.T) {
	cfg := DefaultConfig()
	err := cfg.Set("does-not-exist", "value")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if !strings.Contains(err.Error(), "unknown config key") {
		t.Errorf("error should mention unknown key: %v", err)
	}
}

// ---- Get ------------------------------------------------------------------

func TestGet_AllKeys(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Model.Name = "openai/gpt-4o"
	cfg.Providers["openrouter"] = "or-key"
	cfg.OpenRouterBaseURL = "https://example.test/api/v1"
	cfg.WalletSeed = "seed phrase"
	cfg.CycleTimeout = "15m"
	cfg.UserGoals = "do stuff"

	tests := []struct{ key, want string }{
		{"model.name", "openai/gpt-4o"},
		{"provider.openrouter", "or-key"},
		{"openrouter.base-url", "https://example.test/api/v1"},
		{"wallet-seed", "seed phrase"},
		{"cycle-timeout", "15m"},
		{"user-goals", "do stuff"},
		{"solana.rpc-url", "https://api.mainnet-beta.solana.com"},
		{"solana.commitment", "confirmed"},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			got, err := cfg.Get(tc.key)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGet_UnknownKey(t *testing.T) {
	cfg := DefaultConfig()
	_, err := cfg.Get("no-such-key")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
}

// ---- Save / Load ----------------------------------------------------------

func TestSave_Load_RoundTrip(t *testing.T) {
	useTempHome(t)

	cfg := DefaultConfig()
	cfg.Model.Name = "anthropic/claude-sonnet-4"
	cfg.Providers["openrouter"] = "sk-or-test"
	cfg.OpenRouterBaseURL = "https://example.test/api/v1"
	cfg.UserGoals = "analyze Solana DeFi"
	cfg.CycleTimeout = "30m"

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Model.Name != cfg.Model.Name {
		t.Errorf("Model.Name: got %q, want %q", loaded.Model.Name, cfg.Model.Name)
	}
	if loaded.Providers["openrouter"] != "sk-or-test" {
		t.Errorf("Providers[openrouter]: got %q", loaded.Providers["openrouter"])
	}
	if loaded.OpenRouterBaseURL != cfg.OpenRouterBaseURL {
		t.Errorf("OpenRouterBaseURL: got %q", loaded.OpenRouterBaseURL)
	}
	if loaded.UserGoals != cfg.UserGoals {
		t.Errorf("UserGoals: got %q, want %q", loaded.UserGoals, cfg.UserGoals)
	}
	if loaded.CycleTimeout != "30m" {
		t.Errorf("CycleTimeout: got %q", loaded.CycleTimeout)
	}
}

func TestLoad_CreatesDefaultWhenAbsent(t *testing.T) {
	useTempHome(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Defaults should be in place.
	if cfg.CycleTimeout != "5m" {
		t.Errorf("CycleTimeout: got %q", cfg.CycleTimeout)
	}

	// config.json should now exist on disk.
	if _, err := os.Stat(ConfigPath()); err != nil {
		t.Errorf("config file not created: %v", err)
	}
}

func TestLoad_ReturnsDefaultsForMissingFields(t *testing.T) {
	useTempHome(t)

	// Write a minimal config missing optional fields.
	partial := `{"model":{"name":"openai/gpt-4o"}}`
	if err := os.MkdirAll(Dir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath(), []byte(partial), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model.Name != "openai/gpt-4o" {
		t.Errorf("Model.Name: got %q", cfg.Model.Name)
	}
	// Defaults from DefaultConfig should apply for missing fields.
	if cfg.CycleTimeout != "5m" {
		t.Errorf("CycleTimeout: got %q, want 5m", cfg.CycleTimeout)
	}
}

func TestLoad_RejectsInvalidJSON(t *testing.T) {
	useTempHome(t)

	if err := os.MkdirAll(Dir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath(), []byte("{not valid json"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSave_WritesValidJSON(t *testing.T) {
	useTempHome(t)

	cfg := DefaultConfig()
	cfg.Model.Name = "openai/gpt-4o"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Errorf("saved config is not valid JSON: %v", err)
	}
}

// ---- Directory helpers ----------------------------------------------------

func TestDir_ContainsDotSolai(t *testing.T) {
	useTempHome(t)
	d := Dir()
	if !strings.HasSuffix(d, ".solai") {
		t.Errorf("Dir() = %q, expected to end with .solai", d)
	}
}

func TestToolsDir_UnderDir(t *testing.T) {
	useTempHome(t)
	if filepath.Dir(ToolsDir()) != Dir() {
		t.Errorf("ToolsDir() %q is not under Dir() %q", ToolsDir(), Dir())
	}
	if filepath.Base(ToolsDir()) != "tools" {
		t.Errorf("ToolsDir() base: got %q, want tools", filepath.Base(ToolsDir()))
	}
}

func TestConfigPath_IsJSONFile(t *testing.T) {
	useTempHome(t)
	if !strings.HasSuffix(ConfigPath(), ".json") {
		t.Errorf("ConfigPath() = %q, expected .json suffix", ConfigPath())
	}
	if filepath.Dir(ConfigPath()) != Dir() {
		t.Error("ConfigPath() should be inside Dir()")
	}
}

// ---- tool-env Set / Get / ToolEnvFor --------------------------------------

func TestSet_ToolEnv(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Set("tool-env.birdeye.BIRDEYE_API_KEY", "secret123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ToolEnv["birdeye"]["BIRDEYE_API_KEY"] != "secret123" {
		t.Errorf("got %q, want %q", cfg.ToolEnv["birdeye"]["BIRDEYE_API_KEY"], "secret123")
	}
}

func TestSet_ToolEnv_MultipleTools(t *testing.T) {
	cfg := DefaultConfig()
	_ = cfg.Set("tool-env.birdeye.BIRDEYE_API_KEY", "abc")
	_ = cfg.Set("tool-env.jupiter.JUPITER_KEY", "xyz")
	if cfg.ToolEnv["birdeye"]["BIRDEYE_API_KEY"] != "abc" {
		t.Error("birdeye key not set")
	}
	if cfg.ToolEnv["jupiter"]["JUPITER_KEY"] != "xyz" {
		t.Error("jupiter key not set")
	}
}

func TestSet_ToolEnv_MalformedKey_MissingVar(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Set("tool-env.birdeye.", "val"); err == nil {
		t.Fatal("expected error for missing var name")
	}
}

func TestSet_ToolEnv_MalformedKey_MissingTool(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Set("tool-env..SOME_VAR", "val"); err == nil {
		t.Fatal("expected error for missing tool name")
	}
}

func TestGet_ToolEnv(t *testing.T) {
	cfg := DefaultConfig()
	_ = cfg.Set("tool-env.birdeye.BIRDEYE_API_KEY", "secret123")
	val, err := cfg.Get("tool-env.birdeye.BIRDEYE_API_KEY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "secret123" {
		t.Errorf("got %q, want %q", val, "secret123")
	}
}

func TestGet_ToolEnv_MissingVar_ReturnsEmpty(t *testing.T) {
	cfg := DefaultConfig()
	val, err := cfg.Get("tool-env.birdeye.BIRDEYE_API_KEY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string, got %q", val)
	}
}

func TestToolEnvFor_ReturnsKeyValuePairs(t *testing.T) {
	cfg := DefaultConfig()
	_ = cfg.Set("tool-env.birdeye.BIRDEYE_API_KEY", "abc123")
	_ = cfg.Set("tool-env.birdeye.BIRDEYE_BASE_URL", "https://example.com")

	env := cfg.ToolEnvFor("birdeye")
	if len(env) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(env), env)
	}
	envMap := make(map[string]string)
	for _, kv := range env {
		parts := strings.SplitN(kv, "=", 2)
		envMap[parts[0]] = parts[1]
	}
	if envMap["BIRDEYE_API_KEY"] != "abc123" {
		t.Errorf("BIRDEYE_API_KEY: got %q", envMap["BIRDEYE_API_KEY"])
	}
	if envMap["BIRDEYE_BASE_URL"] != "https://example.com" {
		t.Errorf("BIRDEYE_BASE_URL: got %q", envMap["BIRDEYE_BASE_URL"])
	}
}

func TestToolEnvFor_UnknownTool_ReturnsNil(t *testing.T) {
	cfg := DefaultConfig()
	if env := cfg.ToolEnvFor("no-such-tool"); env != nil {
		t.Errorf("expected nil, got %v", env)
	}
}

func TestToolEnvFor_NilToolEnv_ReturnsNil(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ToolEnv = nil
	if env := cfg.ToolEnvFor("birdeye"); env != nil {
		t.Errorf("expected nil for nil ToolEnv, got %v", env)
	}
}

func TestSave_Load_ToolEnv_RoundTrip(t *testing.T) {
	useTempHome(t)
	cfg := DefaultConfig()
	_ = cfg.Set("tool-env.birdeye.BIRDEYE_API_KEY", "secret")
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ToolEnv["birdeye"]["BIRDEYE_API_KEY"] != "secret" {
		t.Errorf("round-trip: got %q, want %q", loaded.ToolEnv["birdeye"]["BIRDEYE_API_KEY"], "secret")
	}
}
