package ipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteOutputTo_AtomicAndReadable(t *testing.T) {
	dir := t.TempDir()
	if err := WriteOutputTo(dir, Output{Type: TypeSuccess, Payload: json.RawMessage(`{"ok":true}`)}); err != nil {
		t.Fatalf("WriteOutputTo: %v", err)
	}

	// The temp file must not linger.
	if _, err := os.Stat(filepath.Join(dir, outputFile+tmpSuffix)); !os.IsNotExist(err) {
		t.Errorf("temp file should have been renamed away: %v", err)
	}

	out, present, err := ReadOutput(dir)
	if err != nil || !present {
		t.Fatalf("ReadOutput: present=%v err=%v", present, err)
	}
	if out.Type != TypeSuccess || string(out.Payload) != `{"ok":true}` {
		t.Errorf("unexpected output: %+v", out)
	}
}

func TestWriteHelpers(t *testing.T) {
	dir := t.TempDir()

	if err := WriteOutputTo(dir, Output{}); err != nil {
		t.Fatal(err)
	}
	// Empty type is rejected on read.
	if _, present, err := ReadOutput(dir); !present || err == nil {
		t.Errorf("expected empty-type error, present=%v err=%v", present, err)
	}

	if err := writePayloadTo(dir, TypeError, "boom"); err != nil {
		t.Fatal(err)
	}
	out, _, err := ReadOutput(dir)
	if err != nil {
		t.Fatal(err)
	}
	var msg string
	if err := json.Unmarshal(out.Payload, &msg); err != nil || msg != "boom" {
		t.Errorf("error payload: %q (%v)", out.Payload, err)
	}
}

func TestReadOutput_MissingAndMalformed(t *testing.T) {
	dir := t.TempDir()

	if _, present, err := ReadOutput(dir); present || err != nil {
		t.Errorf("missing file: present=%v err=%v", present, err)
	}

	if err := os.WriteFile(filepath.Join(dir, outputFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, present, err := ReadOutput(dir); !present || err == nil {
		t.Errorf("malformed file: present=%v err=%v", present, err)
	}
}

func TestReadInputFrom(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadInputFrom(dir); err == nil {
		t.Error("expected error for missing input.json")
	}

	raw, _ := json.Marshal(Input{Prompt: "do it", Payload: map[string]string{"k": "v"}})
	if err := os.WriteFile(filepath.Join(dir, inputFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := ReadInputFrom(dir)
	if err != nil {
		t.Fatalf("ReadInputFrom: %v", err)
	}
	if in.Prompt != "do it" || in.Payload["k"] != "v" {
		t.Errorf("unexpected input: %+v", in)
	}
}

func TestWriteOutput_NoDirEnv(t *testing.T) {
	t.Setenv(EnvDir, "")
	if err := WriteOutput(Output{Type: TypeSuccess}); err == nil {
		t.Error("expected error when SOLAI_IPC_DIR is unset")
	}
}

// writePayloadTo is a test helper mirroring writePayload but writing to an
// explicit directory.
func writePayloadTo(dir string, typ Type, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return WriteOutputTo(dir, Output{Type: typ, Payload: raw})
}
