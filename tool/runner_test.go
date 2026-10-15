package tool

import (
	"testing"
)

// ---- parseTaskInput ---------------------------------------------------------

func TestParseTaskInput_ValidJSON(t *testing.T) {
	input := `{"prompt":"fetch prices","payload":{"wallet_address":"ABC"}}`
	got := parseTaskInput(input)
	if got.Prompt != "fetch prices" {
		t.Errorf("Prompt: got %q, want %q", got.Prompt, "fetch prices")
	}
	if got.Payload["wallet_address"] != "ABC" {
		t.Errorf("Payload[wallet_address]: got %q, want %q", got.Payload["wallet_address"], "ABC")
	}
}

func TestParseTaskInput_PlainString(t *testing.T) {
	input := "get the current SOL price"
	got := parseTaskInput(input)
	if got.Prompt != input {
		t.Errorf("Prompt: got %q, want %q", got.Prompt, input)
	}
	if len(got.Payload) != 0 {
		t.Errorf("Payload: expected empty, got %v", got.Payload)
	}
}

func TestParseTaskInput_JSONWithNoPrompt_FallsBack(t *testing.T) {
	// Valid JSON but no prompt field — fall back to plain-string path.
	input := `{"payload":{"key":"val"}}`
	got := parseTaskInput(input)
	if got.Prompt != input {
		t.Errorf("expected fallback to plain string, got Prompt=%q", got.Prompt)
	}
}

func TestParseTaskInput_InvalidJSON_FallsBack(t *testing.T) {
	input := "{broken json"
	got := parseTaskInput(input)
	if got.Prompt != input {
		t.Errorf("expected fallback, got Prompt=%q", got.Prompt)
	}
	if len(got.Payload) != 0 {
		t.Errorf("unexpected Payload: %v", got.Payload)
	}
}

func TestParseTaskInput_EmptyString(t *testing.T) {
	got := parseTaskInput("")
	if got.Prompt != "" {
		t.Errorf("Prompt: got %q, want empty", got.Prompt)
	}
}

func TestParseTaskInput_JSONWithPayloadPreserved(t *testing.T) {
	input := `{"prompt":"do thing","payload":{"a":"1","b":"2","c":"3"}}`
	got := parseTaskInput(input)
	if len(got.Payload) != 3 {
		t.Errorf("expected 3 payload entries, got %d", len(got.Payload))
	}
}
