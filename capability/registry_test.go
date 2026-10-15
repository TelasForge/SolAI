package capability

import (
	"context"
	"strings"
	"testing"
)

// freshRegistry saves and restores the global registry for test isolation.
func freshRegistry(t *testing.T) {
	t.Helper()
	saved := registry
	registry = make(map[string]Factory)
	t.Cleanup(func() { registry = saved })
}

// stubCapability is a minimal Capability for testing.
type stubCapability struct {
	name  string
	class CapabilityClass
	desc  string
}

func (s *stubCapability) Name() string                                        { return s.name }
func (s *stubCapability) Class() CapabilityClass                              { return s.class }
func (s *stubCapability) Description() string                                 { return s.desc }
func (s *stubCapability) ToolRequestDescription() string                      { return "" }
func (s *stubCapability) Execute(_ context.Context, _ string) (string, error) { return s.name, nil }

// ---- Register / SetUp -------------------------------------------------------

func TestRegister_SetUp_BasicCapability(t *testing.T) {
	freshRegistry(t)

	Register("test-cap", func() Capability {
		return &stubCapability{name: "test-cap", class: Internal, desc: "test"}
	})
	cm := SetUp([]string{"test-cap"})
	all := cm.GetAll()
	if len(all) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(all))
	}
	if all[0].Name() != "test-cap" {
		t.Errorf("got name %q, want %q", all[0].Name(), "test-cap")
	}
}

func TestSetUp_UnknownNameIgnored(t *testing.T) {
	freshRegistry(t)

	cm := SetUp([]string{"does-not-exist"})
	if len(cm.GetAll()) != 0 {
		t.Error("expected 0 capabilities for unknown name")
	}
}

func TestSetUp_MultipleCapabilities(t *testing.T) {
	freshRegistry(t)

	Register("cap-a", func() Capability { return &stubCapability{name: "cap-a", class: Core} })
	Register("cap-b", func() Capability { return &stubCapability{name: "cap-b", class: Internal} })
	Register("cap-c", func() Capability { return &stubCapability{name: "cap-c", class: Regular} })

	cm := SetUp([]string{"cap-a", "cap-b", "cap-c"})
	if len(cm.GetAll()) != 3 {
		t.Fatalf("expected 3 capabilities, got %d", len(cm.GetAll()))
	}
}

func TestSetUp_EmptyNames(t *testing.T) {
	freshRegistry(t)
	cm := SetUp(nil)
	if len(cm.GetAll()) != 0 {
		t.Error("expected 0 capabilities for empty names")
	}
}

// ---- GetByClass -------------------------------------------------------------

func TestGetByClass_FiltersCorrectly(t *testing.T) {
	freshRegistry(t)

	Register("core-cap", func() Capability { return &stubCapability{name: "core-cap", class: Core} })
	Register("int-cap", func() Capability { return &stubCapability{name: "int-cap", class: Internal} })
	Register("reg-cap", func() Capability { return &stubCapability{name: "reg-cap", class: Regular} })

	cm := SetUp([]string{"core-cap", "int-cap", "reg-cap"})

	if len(cm.GetByClass(Core)) != 1 {
		t.Errorf("expected 1 Core capability, got %d", len(cm.GetByClass(Core)))
	}
	if len(cm.GetByClass(Internal)) != 1 {
		t.Errorf("expected 1 Internal capability, got %d", len(cm.GetByClass(Internal)))
	}
	if len(cm.GetByClass(Regular)) != 1 {
		t.Errorf("expected 1 Regular capability, got %d", len(cm.GetByClass(Regular)))
	}
}

func TestGetByClass_EmptyResult(t *testing.T) {
	freshRegistry(t)
	cm := SetUp(nil)
	if got := cm.GetByClass(Internal); len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

// ---- IsRegularCapabilityAvailable -------------------------------------------

func TestIsRegularCapabilityAvailable_True(t *testing.T) {
	freshRegistry(t)
	Register("network-manager", func() Capability {
		return &stubCapability{name: "network-manager", class: Regular}
	})
	cm := SetUp([]string{"network-manager"})
	if !cm.IsRegularCapabilityAvailable("network-manager") {
		t.Error("expected network-manager to be available")
	}
}

func TestIsRegularCapabilityAvailable_WrongClass(t *testing.T) {
	freshRegistry(t)
	Register("wallet", func() Capability {
		return &stubCapability{name: "wallet", class: Internal}
	})
	cm := SetUp([]string{"wallet"})
	if cm.IsRegularCapabilityAvailable("wallet") {
		t.Error("Internal capability should not be reported as Regular available")
	}
}

func TestIsRegularCapabilityAvailable_NotPresent(t *testing.T) {
	freshRegistry(t)
	cm := SetUp(nil)
	if cm.IsRegularCapabilityAvailable("file-manager") {
		t.Error("expected false for unregistered capability")
	}
}

// ---- BuildCapabilityPromptSection -------------------------------------------

func TestBuildCapabilityPromptSection_InternalAndRegular(t *testing.T) {
	freshRegistry(t)
	Register("wallet", func() Capability {
		return &stubCapability{name: "wallet", class: Internal, desc: "your wallet is 0xabc"}
	})
	Register("net", func() Capability {
		return &stubCapability{name: "net", class: Regular, desc: "network"}
	})
	cm := SetUp([]string{"wallet", "net"})

	section := cm.BuildCapabilityPromptSection()
	if section == "" {
		t.Fatal("expected non-empty section for Internal+Regular capabilities")
	}
	if !strings.Contains(section, "wallet") {
		t.Errorf("expected wallet in section: %q", section)
	}
	if !strings.Contains(section, "your wallet is 0xabc") {
		t.Errorf("expected wallet description in section: %q", section)
	}
	// Regular capability also appears in the prompt section.
	if !strings.Contains(section, "network") {
		t.Errorf("expected Regular capability in section: %q", section)
	}
}

func TestBuildCapabilityPromptSection_RegularOnly(t *testing.T) {
	freshRegistry(t)
	Register("net", func() Capability {
		return &stubCapability{name: "net", class: Regular, desc: "network"}
	})
	cm := SetUp([]string{"net"})
	// Regular capabilities are visible to the coordinator LLM.
	if section := cm.BuildCapabilityPromptSection(); section == "" {
		t.Error("expected non-empty section for Regular capability")
	}
}

func TestBuildCapabilityPromptSection_Empty(t *testing.T) {
	freshRegistry(t)
	cm := SetUp(nil)
	if section := cm.BuildCapabilityPromptSection(); section != "" {
		t.Errorf("expected empty section, got %q", section)
	}
}
