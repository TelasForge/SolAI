package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/melodyogonna/solai/ipc"
)

func writeScript(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "tool.sh"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runTool(t *testing.T, scriptDir, ipcDir string) (ipc.Output, error) {
	t.Helper()
	return RunTool(context.Background(), scriptDir, "tool.sh",
		ipc.Input{Prompt: "do it"}, 5*time.Second,
		[]string{ipc.EnvDir + "=" + ipcDir}, ipcDir)
}

func TestRunTool_WritesInputAndReadsResult(t *testing.T) {
	scriptDir := t.TempDir()
	ipcDir := t.TempDir()
	writeScript(t, scriptDir, `#!/bin/sh
printf '%s' '{"type":"success","payload":{"ok":true}}' > "$SOLAI_IPC_DIR/output.json.tmp"
mv "$SOLAI_IPC_DIR/output.json.tmp" "$SOLAI_IPC_DIR/output.json"
`)

	out, err := runTool(t, scriptDir, ipcDir)
	if err != nil {
		t.Fatalf("RunTool: %v", err)
	}
	if out.Type != ipc.TypeSuccess || string(out.Payload) != `{"ok":true}` {
		t.Errorf("unexpected output: %+v", out)
	}

	data, err := os.ReadFile(filepath.Join(ipcDir, "input.json"))
	if err != nil || !strings.Contains(string(data), "do it") {
		t.Errorf("input.json not written: %s (%v)", data, err)
	}
}

func TestRunTool_NonZeroExitWithResultIsAccepted(t *testing.T) {
	scriptDir := t.TempDir()
	ipcDir := t.TempDir()
	writeScript(t, scriptDir, `#!/bin/sh
printf '%s' '{"type":"error","payload":"boom"}' > "$SOLAI_IPC_DIR/output.json"
exit 3
`)

	out, err := runTool(t, scriptDir, ipcDir)
	if err != nil {
		t.Fatalf("expected the structured result to be accepted, got %v", err)
	}
	if out.Type != ipc.TypeError {
		t.Fatalf("type: got %q", out.Type)
	}
	var msg string
	if err := json.Unmarshal(out.Payload, &msg); err != nil || msg != "boom" {
		t.Errorf("payload: %s (%v)", out.Payload, err)
	}
}

func TestRunTool_NonZeroExitNoResultSurfacesStderr(t *testing.T) {
	scriptDir := t.TempDir()
	ipcDir := t.TempDir()
	writeScript(t, scriptDir, `#!/bin/sh
echo "kaboom" >&2
exit 3
`)

	_, err := runTool(t, scriptDir, ipcDir)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "kaboom") {
		t.Errorf("stderr not surfaced: %v", err)
	}
}

func TestRunTool_NoResultIsError(t *testing.T) {
	scriptDir := t.TempDir()
	ipcDir := t.TempDir()
	writeScript(t, scriptDir, "#!/bin/sh\nexit 0\n")

	_, err := runTool(t, scriptDir, ipcDir)
	if err == nil || !strings.Contains(err.Error(), "no result") {
		t.Errorf("expected missing-result error, got %v", err)
	}
}

func TestRunTool_MalformedResultIsError(t *testing.T) {
	scriptDir := t.TempDir()
	ipcDir := t.TempDir()
	writeScript(t, scriptDir, `#!/bin/sh
printf '%s' '{not json' > "$SOLAI_IPC_DIR/output.json"
`)

	_, err := runTool(t, scriptDir, ipcDir)
	if err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Errorf("expected parse error, got %v", err)
	}
}
