package capability

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/melodyogonna/solai/ipc"
)

type brokerStub struct {
	name     string
	class    CapabilityClass
	gotInput string
	result   string
	err      error
}

func (s *brokerStub) Name() string                   { return s.name }
func (s *brokerStub) Class() CapabilityClass         { return s.class }
func (s *brokerStub) Description() string            { return "" }
func (s *brokerStub) ToolRequestDescription() string { return "" }
func (s *brokerStub) Execute(_ context.Context, input string) (string, error) {
	s.gotInput = input
	return s.result, s.err
}

func managerWith(caps ...Capability) *CapabilityManager {
	return &CapabilityManager{capabilities: caps}
}

func TestBroker_ExecutesRegularCapability(t *testing.T) {
	stub := &brokerStub{name: "wallet", class: Regular, result: "SIGNED"}
	b := NewBroker(managerWith(stub))

	out, err := b.Execute(context.Background(), ipc.Request{
		Capability: "wallet",
		Action:     "sign",
		Input:      "abc",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "SIGNED" {
		t.Errorf("result: got %q", out)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(stub.gotInput), &got); err != nil {
		t.Fatalf("input not JSON: %q", stub.gotInput)
	}
	if got["action"] != "sign" || got["input"] != "abc" {
		t.Errorf("capability received wrong input: %v", got)
	}
}

func TestBroker_UnknownCapability(t *testing.T) {
	b := NewBroker(managerWith())
	_, err := b.Execute(context.Background(), ipc.Request{Capability: "nope"})
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Errorf("expected not-available error, got %v", err)
	}
}

func TestBroker_NonRegularCapabilityRejected(t *testing.T) {
	stub := &brokerStub{name: "memory", class: Internal}
	b := NewBroker(managerWith(stub))
	_, err := b.Execute(context.Background(), ipc.Request{Capability: "memory"})
	if err == nil || !strings.Contains(err.Error(), "not requestable") {
		t.Errorf("expected not-requestable error, got %v", err)
	}
}

func TestBroker_MissingName(t *testing.T) {
	b := NewBroker(managerWith())
	_, err := b.Execute(context.Background(), ipc.Request{})
	if err == nil || !strings.Contains(err.Error(), "missing a capability name") {
		t.Errorf("expected missing-name error, got %v", err)
	}
}

func TestBroker_NilManager(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.Execute(context.Background(), ipc.Request{Capability: "wallet"}); err == nil {
		t.Error("expected error with nil manager")
	}
}
