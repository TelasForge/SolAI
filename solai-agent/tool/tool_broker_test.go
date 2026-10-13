package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/melodyogonna/solai/ipc"
	"github.com/melodyogonna/solai/solai-agent/capability"
)

type fakeBroker struct {
	calls  int
	got    ipc.Request
	result string
	err    error
}

func (b *fakeBroker) Execute(_ context.Context, req ipc.Request) (string, error) {
	b.calls++
	b.got = req
	return b.result, b.err
}

// requestTool creates an AgenticTool whose executable emits the given output
// for one invocation.
func requestTool(t *testing.T, output string, broker Broker) *AgenticTool {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s' '" + output + "' > \"$SOLAI_IPC_DIR/output.json\"\n"
	if err := os.WriteFile(filepath.Join(dir, "tool.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	m := Manifest{Name: "t", Description: "d", Version: "1.0.0", Executable: "./tool.sh"}
	return NewAgenticTool(m, dir, nil, nil, capability.NewCommunicationCapability(), nil, "", broker)
}

func TestAgenticTool_BrokersCapabilityRequest(t *testing.T) {
	broker := &fakeBroker{result: "SIGNED"}
	output := `{"type":"request","payload":{"capability":"wallet","action":"sign","input":"abc","instruction":"re-invoke with the signature"}}`
	at := requestTool(t, output, broker)

	out, err := at.Call(context.Background(), `{"prompt":"go"}`)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if broker.calls != 1 {
		t.Fatalf("expected 1 broker call, got %d", broker.calls)
	}
	if broker.got.Capability != "wallet" || broker.got.Action != "sign" || broker.got.Input != "abc" {
		t.Errorf("broker received wrong request: %+v", broker.got)
	}
	if !strings.Contains(out, "SIGNED") {
		t.Errorf("capability result missing from output: %q", out)
	}
	if !strings.Contains(out, "re-invoke with the signature") {
		t.Errorf("instruction missing from output: %q", out)
	}
}

func TestAgenticTool_BrokerErrorIsReported(t *testing.T) {
	broker := &fakeBroker{err: context.DeadlineExceeded}
	output := `{"type":"request","payload":{"capability":"wallet","action":"sign"}}`
	at := requestTool(t, output, broker)

	out, err := at.Call(context.Background(), `{"prompt":"go"}`)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !strings.Contains(out, "could not be executed") {
		t.Errorf("expected failure in output: %q", out)
	}
}

func TestAgenticTool_NoBrokerReturnsRawRequest(t *testing.T) {
	output := `{"type":"request","payload":{"capability":"wallet","action":"sign"}}`
	at := requestTool(t, output, nil)

	out, err := at.Call(context.Background(), `{"prompt":"go"}`)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !strings.Contains(out, `"capability":"wallet"`) {
		t.Errorf("expected raw request passthrough: %q", out)
	}
}
