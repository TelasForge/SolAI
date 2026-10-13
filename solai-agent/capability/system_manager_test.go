package capability

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/melodyogonna/solai/llm"
)

// stubTool implements llm.Tool for testing.
type stubTool struct{ name string }

func (s *stubTool) Name() string                                     { return s.name }
func (s *stubTool) Description() string                              { return s.name }
func (s *stubTool) Call(_ context.Context, _ string) (string, error) { return "", nil }

// ---- Name / Class / Description ---------------------------------------------

func TestSystemManager_Metadata(t *testing.T) {
	sm := NewSystemManager(nil, NewLLMProvider("", ""))
	if sm.Name() != "system-manager" {
		t.Errorf("Name: got %q, want %q", sm.Name(), "system-manager")
	}
	if sm.Class() != Core {
		t.Errorf("Class: got %v, want Core", sm.Class())
	}
	if sm.Description() != "" {
		t.Errorf("Description: expected empty for Core capability, got %q", sm.Description())
	}
}

// ---- GetTools panics before Setup -------------------------------------------

func TestGetTools_PanicsIfSetupNotCalled(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when GetTools called before Setup")
		}
	}()
	sm := NewSystemManager(nil, NewLLMProvider("", ""))
	sm.GetTools() // must panic
}

// ---- Execute ----------------------------------------------------------------

func TestExecute_BeforeSetup_ReturnsEmptyTools(t *testing.T) {
	sm := NewSystemManager(nil, NewLLMProvider("", ""))
	result, err := sm.Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(result), &status); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if status["tools_loaded"].(float64) != 0 {
		t.Errorf("expected 0 tools loaded, got %v", status["tools_loaded"])
	}
}

func TestExecute_AfterSetup_ReflectsLoadedTools(t *testing.T) {
	tools := []llm.Tool{
		&stubTool{name: "tool-a"},
		&stubTool{name: "tool-b"},
	}
	loader := func(_ *CapabilityManager) ([]llm.Tool, []error, error) {
		return tools, nil, nil
	}
	sm := NewSystemManager(loader, NewLLMProvider("", ""))
	if _, err := sm.Setup(SetUp(nil)); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	result, err := sm.Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(result), &status); err != nil {
		t.Fatalf("result not valid JSON: %v", err)
	}
	if int(status["tools_loaded"].(float64)) != len(tools) {
		t.Errorf("tools_loaded: got %v, want %d", status["tools_loaded"], len(tools))
	}
}

// ---- Setup ------------------------------------------------------------------

func TestSetup_LoaderFatalError_Propagated(t *testing.T) {
	loader := func(_ *CapabilityManager) ([]llm.Tool, []error, error) {
		return nil, nil, errors.New("fatal loader error")
	}
	sm := NewSystemManager(loader, NewLLMProvider("", ""))
	_, err := sm.Setup(SetUp(nil))
	if err == nil {
		t.Fatal("expected fatal error from loader to be propagated")
	}
}

func TestSetup_LoaderWarnings_Returned(t *testing.T) {
	loader := func(_ *CapabilityManager) ([]llm.Tool, []error, error) {
		return nil, []error{errors.New("tool-x disabled")}, nil
	}
	sm := NewSystemManager(loader, NewLLMProvider("", ""))
	warnings, err := sm.Setup(SetUp(nil))
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(warnings))
	}
}

func TestSetup_GetToolsAfterSetup(t *testing.T) {
	expected := []llm.Tool{&stubTool{name: "my-tool"}}
	loader := func(_ *CapabilityManager) ([]llm.Tool, []error, error) {
		return expected, nil, nil
	}
	sm := NewSystemManager(loader, NewLLMProvider("", ""))
	if _, err := sm.Setup(SetUp(nil)); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	tools := sm.GetTools()
	if len(tools) != len(expected) {
		t.Errorf("GetTools: got %d tools, want %d", len(tools), len(expected))
	}
}

// ---- RegisterJob / Start ----------------------------------------------------

func TestRegisterJob_RunsOnTick(t *testing.T) {
	sm := NewSystemManager(nil, NewLLMProvider("", ""))

	ran := make(chan struct{}, 1)
	sm.RegisterJob(CleanupJob{
		Name:     "test-job",
		Interval: 10 * time.Millisecond,
		Fn: func(_ context.Context) error {
			select {
			case ran <- struct{}{}:
			default:
			}
			return nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go sm.Start(ctx)

	select {
	case <-ran:
		// job ran at least once
	case <-ctx.Done():
		t.Error("job did not run within timeout")
	}
}

func TestRegisterJob_StopsOnContextCancel(t *testing.T) {
	sm := NewSystemManager(nil, NewLLMProvider("", ""))

	count := 0
	sm.RegisterJob(CleanupJob{
		Name:     "count-job",
		Interval: 10 * time.Millisecond,
		Fn: func(_ context.Context) error {
			count++
			return nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Start blocks until context is done.
	sm.Start(ctx)
	// After Start returns, no more increments should happen.
	before := count
	time.Sleep(20 * time.Millisecond)
	if count != before {
		t.Errorf("job ran after context was cancelled: count changed from %d to %d", before, count)
	}
}
