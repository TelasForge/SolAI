package tool

import (
	"strings"
	"testing"
	"time"

	"github.com/melodyogonna/solai/capability"
)

func makeManifest(name, desc, executable, timeout string) Manifest {
	return Manifest{
		Name:        name,
		Description: desc,
		Executable:  executable,
		Timeout:     timeout,
	}
}

func newTestAgenticTool(m Manifest, llmCfg *capability.LLMConfig) *AgenticTool {
	return NewAgenticTool(m, "/tools/"+m.Name, llmCfg, nil, capability.NewCommunicationCapability(), nil, "", nil)
}

// ---- NewAgenticTool — timeout parsing ---------------------------------------

func TestNewAgenticTool_DefaultTimeout_WhenEmpty(t *testing.T) {
	m := makeManifest("my-tool", "desc", "./bin/my-tool", "")
	at := newTestAgenticTool(m, nil)
	if at.timeout != DefaultToolTimeout {
		t.Errorf("timeout: got %v, want %v", at.timeout, DefaultToolTimeout)
	}
}

func TestNewAgenticTool_ParsesValidTimeout(t *testing.T) {
	m := makeManifest("my-tool", "desc", "./bin/my-tool", "90s")
	at := newTestAgenticTool(m, nil)
	if at.timeout != 90*time.Second {
		t.Errorf("timeout: got %v, want 90s", at.timeout)
	}
}

func TestNewAgenticTool_DefaultTimeout_WhenInvalid(t *testing.T) {
	m := makeManifest("my-tool", "desc", "./bin/my-tool", "notaduration")
	at := newTestAgenticTool(m, nil)
	if at.timeout != DefaultToolTimeout {
		t.Errorf("timeout: got %v, want default %v", at.timeout, DefaultToolTimeout)
	}
}

func TestNewAgenticTool_DefaultTimeout_WhenZero(t *testing.T) {
	m := makeManifest("my-tool", "desc", "./bin/my-tool", "0s")
	at := newTestAgenticTool(m, nil)
	if at.timeout != DefaultToolTimeout {
		t.Errorf("timeout: got %v, want default %v (zero duration should fall back)", at.timeout, DefaultToolTimeout)
	}
}

func TestNewAgenticTool_ParsesMinuteTimeout(t *testing.T) {
	m := makeManifest("my-tool", "desc", "./bin/my-tool", "2m")
	at := newTestAgenticTool(m, nil)
	if at.timeout != 2*time.Minute {
		t.Errorf("timeout: got %v, want 2m", at.timeout)
	}
}

// ---- Name / Description / llmCfg --------------------------------------------

func TestAgenticTool_Name(t *testing.T) {
	m := makeManifest("token-price", "desc", "./bin/token-price", "")
	at := newTestAgenticTool(m, nil)
	if at.Name() != "token-price" {
		t.Errorf("Name: got %q, want token-price", at.Name())
	}
}

func TestAgenticTool_Description(t *testing.T) {
	m := makeManifest("token-price", "Fetches USD prices for Solana tokens.", "./bin/token-price", "")
	at := newTestAgenticTool(m, nil)
	if at.Description() != "Fetches USD prices for Solana tokens." {
		t.Errorf("Description: got %q", at.Description())
	}
}

func TestAgenticTool_Description_AppendsPayloads(t *testing.T) {
	m := makeManifest("my-tool", "desc", "./bin/my-tool", "")
	m.Payloads = []ManifestPayload{{Name: "wallet_address", Description: "pubkey", Source: "wallet"}}
	at := newTestAgenticTool(m, nil)
	desc := at.Description()
	if desc == "desc" {
		t.Error("expected payload documentation appended to description")
	}
	if !strings.Contains(desc, "wallet_address") || !strings.Contains(desc, "auto-injected") {
		t.Errorf("unexpected description: %q", desc)
	}
}

func TestAgenticTool_WithLLMConfig(t *testing.T) {
	m := makeManifest("my-tool", "desc", "./bin/my-tool", "")
	llmCfg := &capability.LLMConfig{Model: "openai/gpt-4o", APIKey: "key"}
	at := newTestAgenticTool(m, llmCfg)
	if at.llmCfg == nil {
		t.Fatal("expected llmCfg to be set")
	}
	if at.llmCfg.Model != "openai/gpt-4o" {
		t.Errorf("llmCfg.Model: got %q", at.llmCfg.Model)
	}
}

func TestAgenticTool_NilLLMConfig(t *testing.T) {
	m := makeManifest("my-tool", "desc", "./bin/my-tool", "")
	at := newTestAgenticTool(m, nil)
	if at.llmCfg != nil {
		t.Error("expected llmCfg to be nil")
	}
}

// ---- Parameters -------------------------------------------------------------

func TestAgenticTool_Parameters(t *testing.T) {
	m := makeManifest("my-tool", "desc", "./bin/my-tool", "")
	at := newTestAgenticTool(m, nil)
	p := at.Parameters()
	if p["type"] != "object" {
		t.Errorf("expected object schema, got %v", p["type"])
	}
	if _, ok := p["properties"].(map[string]any)["prompt"]; !ok {
		t.Errorf("expected prompt property, got %v", p["properties"])
	}
}

// ---- DefaultToolTimeout value -----------------------------------------------

func TestDefaultToolTimeout_IsAtLeastOneMinute(t *testing.T) {
	if DefaultToolTimeout < time.Minute {
		t.Errorf("DefaultToolTimeout is %v, expected at least 1 minute for LLM subagent calls", DefaultToolTimeout)
	}
}
