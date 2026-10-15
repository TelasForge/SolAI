package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/melodyogonna/solai/wallet"
)

// ---- WalletCapability -------------------------------------------------------

func newTestWallet(t *testing.T) *wallet.SolKeyPair {
	t.Helper()
	kp, err := wallet.CreateWallet("")
	if err != nil {
		t.Fatalf("CreateWallet: %v", err)
	}
	return &kp
}

func TestWalletCapability_Name(t *testing.T) {
	kp := newTestWallet(t)
	w := NewWalletCapability(kp)
	if w.Name() != "wallet" {
		t.Errorf("Name: got %q, want wallet", w.Name())
	}
}

func TestWalletCapability_Class(t *testing.T) {
	kp := newTestWallet(t)
	w := NewWalletCapability(kp)
	if w.Class() != Regular {
		t.Errorf("Class: got %v, want Regular", w.Class())
	}
}

func TestWalletCapability_Description_ContainsPubKey(t *testing.T) {
	kp := newTestWallet(t)
	w := NewWalletCapability(kp)
	desc := w.Description()

	pubKey := kp.Base58PubKey()
	if !strings.Contains(desc, pubKey) {
		t.Errorf("Description should contain public key %q, got: %q", pubKey, desc)
	}
}

func TestWalletCapability_Description_NonEmpty(t *testing.T) {
	kp := newTestWallet(t)
	w := NewWalletCapability(kp)
	if w.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestWalletCapability_Execute_ReturnsPubKey(t *testing.T) {
	kp := newTestWallet(t)
	w := NewWalletCapability(kp)
	result, err := w.Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != kp.Base58PubKey() {
		t.Errorf("Execute: got %q, want %q", result, kp.Base58PubKey())
	}
}

func TestWalletCapability_Execute_InputIgnored(t *testing.T) {
	kp := newTestWallet(t)
	w := NewWalletCapability(kp)
	r1, _ := w.Execute(context.Background(), "anything")
	r2, _ := w.Execute(context.Background(), "")
	if r1 != r2 {
		t.Errorf("Execute should ignore input: got %q vs %q", r1, r2)
	}
}

func TestWalletCapability_DifferentWallets_DifferentKeys(t *testing.T) {
	kp1 := newTestWallet(t)
	kp2 := newTestWallet(t)
	w1 := NewWalletCapability(kp1)
	w2 := NewWalletCapability(kp2)
	if w1.Description() == w2.Description() {
		t.Error("different wallets should produce different descriptions")
	}
}

// ---- NetworkManagerCapability -----------------------------------------------

func TestNetworkManagerCapability_Name(t *testing.T) {
	n := NewNetworkManagerCapability()
	if n.Name() != "network-manager" {
		t.Errorf("Name: got %q, want network-manager", n.Name())
	}
}

func TestNetworkManagerCapability_Class(t *testing.T) {
	n := NewNetworkManagerCapability()
	if n.Class() != Regular {
		t.Errorf("Class: got %v, want Regular", n.Class())
	}
}

func TestNetworkManagerCapability_Description_Empty(t *testing.T) {
	// network-manager intentionally has an empty description so it is not
	// shown to the coordinator LLM as a callable tool. Network access is
	// declared statically via required_capabilities, not by direct LLM invocation.
	n := NewNetworkManagerCapability()
	if n.Description() != "" {
		t.Errorf("expected empty description, got %q", n.Description())
	}
}

func TestNetworkManagerCapability_Execute_ReturnsJSON(t *testing.T) {
	n := NewNetworkManagerCapability()
	result, err := n.Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Errorf("Execute result is not valid JSON: %v (got %q)", err, result)
	}
}

func TestNetworkManagerCapability_IsNotCore(t *testing.T) {
	n := NewNetworkManagerCapability()
	if n.Class() == Core {
		t.Error("NetworkManager should not be Core class")
	}
}

// ---- MemoryCapability -------------------------------------------------------

func TestMemoryCapability_Metadata(t *testing.T) {
	m := NewMemoryCapability()
	if m.Name() != "memory" {
		t.Errorf("Name: got %q, want memory", m.Name())
	}
	if m.Class() != Internal {
		t.Errorf("Class: got %v, want Internal", m.Class())
	}
	if m.ToolRequestDescription() != "" {
		t.Errorf("ToolRequestDescription: expected empty, got %q", m.ToolRequestDescription())
	}
	if m.Description() == "" {
		t.Error("Description: expected non-empty")
	}
}

func TestMemoryCapability_Execute_UpdatePlan(t *testing.T) {
	m := NewMemoryCapability()
	out, err := m.Execute(context.Background(), `{"action":"update_plan","plan":"monitor SOL price"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertOK(t, out)
	if m.plan != "monitor SOL price" {
		t.Errorf("plan not stored: got %q", m.plan)
	}
}

func TestMemoryCapability_Execute_UpdatePlan_Replaces(t *testing.T) {
	m := NewMemoryCapability()
	m.Execute(context.Background(), `{"action":"update_plan","plan":"old plan"}`)
	m.Execute(context.Background(), `{"action":"update_plan","plan":"new plan"}`)
	if m.plan != "new plan" {
		t.Errorf("expected plan to be replaced: got %q", m.plan)
	}
}

func TestMemoryCapability_Execute_AddObservation(t *testing.T) {
	m := NewMemoryCapability()
	out, err := m.Execute(context.Background(), `{"action":"add_observation","content":"SOL at $180"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertOK(t, out)
	if len(m.observations) != 1 || m.observations[0] != "SOL at $180" {
		t.Errorf("observation not stored: %v", m.observations)
	}
}

func TestMemoryCapability_Execute_AddObservation_RingEvicts(t *testing.T) {
	m := NewMemoryCapability()
	for i := 0; i < maxObservations+3; i++ {
		m.Execute(context.Background(), `{"action":"add_observation","content":"obs"}`)
	}
	if len(m.observations) != maxObservations {
		t.Errorf("ring should cap at %d, got %d", maxObservations, len(m.observations))
	}
}

func TestMemoryCapability_Execute_AddPending(t *testing.T) {
	m := NewMemoryCapability()
	out, err := m.Execute(context.Background(), `{"action":"add_pending","task":"buy JUP"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertOK(t, out)
	if len(m.pending) != 1 || m.pending[0] != "buy JUP" {
		t.Errorf("pending not stored: %v", m.pending)
	}
}

func TestMemoryCapability_Execute_CompleteTask(t *testing.T) {
	m := NewMemoryCapability()
	m.Execute(context.Background(), `{"action":"add_pending","task":"buy JUP"}`)
	out, err := m.Execute(context.Background(), `{"action":"complete_task","task":"buy JUP"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertOK(t, out)
	if len(m.pending) != 0 {
		t.Errorf("pending should be empty after complete_task, got %v", m.pending)
	}
	if len(m.completed) != 1 || m.completed[0] != "buy JUP" {
		t.Errorf("completed not updated: %v", m.completed)
	}
}

func TestMemoryCapability_Execute_CompleteTask_RingEvicts(t *testing.T) {
	m := NewMemoryCapability()
	for i := 0; i < maxCompleted+3; i++ {
		m.Execute(context.Background(), `{"action":"complete_task","task":"t"}`)
	}
	if len(m.completed) != maxCompleted {
		t.Errorf("completed ring should cap at %d, got %d", maxCompleted, len(m.completed))
	}
}

func TestMemoryCapability_Execute_RemovePending(t *testing.T) {
	m := NewMemoryCapability()
	m.Execute(context.Background(), `{"action":"add_pending","task":"task A"}`)
	m.Execute(context.Background(), `{"action":"add_pending","task":"task B"}`)
	out, err := m.Execute(context.Background(), `{"action":"remove_pending","task":"task A"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertOK(t, out)
	if len(m.pending) != 1 || m.pending[0] != "task B" {
		t.Errorf("expected only task B remaining, got %v", m.pending)
	}
}

func TestMemoryCapability_Execute_Read(t *testing.T) {
	m := NewMemoryCapability()
	m.Execute(context.Background(), `{"action":"update_plan","plan":"the plan"}`)
	m.Execute(context.Background(), `{"action":"add_observation","content":"obs1"}`)
	m.Execute(context.Background(), `{"action":"add_pending","task":"do thing"}`)

	out, err := m.Execute(context.Background(), `{"action":"read"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(out), &snap); err != nil {
		t.Fatalf("read result is not valid JSON: %v (got %q)", err, out)
	}
	if snap["plan"] != "the plan" {
		t.Errorf("read plan: got %v", snap["plan"])
	}
	obs, ok := snap["observations"].([]any)
	if !ok || len(obs) != 1 {
		t.Errorf("read observations: got %v", snap["observations"])
	}
	pending, ok := snap["pending"].([]any)
	if !ok || len(pending) != 1 {
		t.Errorf("read pending: got %v", snap["pending"])
	}
}

func TestMemoryCapability_Execute_InvalidJSON(t *testing.T) {
	m := NewMemoryCapability()
	out, err := m.Execute(context.Background(), `not json`)
	if err != nil {
		t.Fatalf("Execute should not return Go error: %v", err)
	}
	if !strings.Contains(out, "error") {
		t.Errorf("expected error in output for bad JSON, got %q", out)
	}
}

func TestMemoryCapability_Execute_UnknownAction(t *testing.T) {
	m := NewMemoryCapability()
	out, err := m.Execute(context.Background(), `{"action":"explode"}`)
	if err != nil {
		t.Fatalf("Execute should not return Go error: %v", err)
	}
	if !strings.Contains(out, "error") {
		t.Errorf("expected error for unknown action, got %q", out)
	}
}

func TestMemoryCapability_Execute_MissingRequiredFields(t *testing.T) {
	m := NewMemoryCapability()
	cases := []string{
		`{"action":"add_observation"}`,
		`{"action":"add_pending"}`,
		`{"action":"complete_task"}`,
		`{"action":"remove_pending"}`,
	}
	for _, input := range cases {
		out, err := m.Execute(context.Background(), input)
		if err != nil {
			t.Errorf("%s: unexpected Go error: %v", input, err)
		}
		if !strings.Contains(out, "error") {
			t.Errorf("%s: expected error in output, got %q", input, out)
		}
	}
}

func TestMemoryCapability_BuildMemorySection_EmptyWhenFresh(t *testing.T) {
	m := NewMemoryCapability()
	if sec := m.BuildMemorySection(); sec != "" {
		t.Errorf("expected empty section on fresh capability, got %q", sec)
	}
}

func TestMemoryCapability_BuildMemorySection_ContainsPlan(t *testing.T) {
	m := NewMemoryCapability()
	m.Execute(context.Background(), `{"action":"update_plan","plan":"watch SOL"}`)
	sec := m.BuildMemorySection()
	if !strings.Contains(sec, "## Agent Memory") {
		t.Errorf("section missing header: %q", sec)
	}
	if !strings.Contains(sec, "watch SOL") {
		t.Errorf("section missing plan text: %q", sec)
	}
}

func TestMemoryCapability_BuildMemorySection_ContainsAllSubsections(t *testing.T) {
	m := NewMemoryCapability()
	m.Execute(context.Background(), `{"action":"update_plan","plan":"p"}`)
	m.Execute(context.Background(), `{"action":"add_observation","content":"o"}`)
	m.Execute(context.Background(), `{"action":"add_pending","task":"t"}`)
	m.Execute(context.Background(), `{"action":"complete_task","task":"done"}`)
	sec := m.BuildMemorySection()
	for _, want := range []string{"### Current Plan", "### Recent Observations", "### Pending Tasks", "### Completed Tasks (recent)"} {
		if !strings.Contains(sec, want) {
			t.Errorf("section missing subsection %q: %q", want, sec)
		}
	}
}

func TestMemoryCapability_ImplementsMemorySectionProvider(t *testing.T) {
	var _ MemorySectionProvider = NewMemoryCapability()
}

func TestMemoryCapability_Execute_CompleteTask_NonExistent(t *testing.T) {
	// Completing a task that was never added should be a no-op, not an error.
	m := NewMemoryCapability()
	out, err := m.Execute(context.Background(), `{"action":"complete_task","task":"ghost"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertOK(t, out)
	if len(m.pending) != 0 {
		t.Errorf("pending should remain empty, got %v", m.pending)
	}
}

func TestMemoryCapability_Execute_RemovePending_NonExistent(t *testing.T) {
	// Removing a task that does not exist should be a no-op.
	m := NewMemoryCapability()
	m.Execute(context.Background(), `{"action":"add_pending","task":"real task"}`)
	out, err := m.Execute(context.Background(), `{"action":"remove_pending","task":"ghost"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertOK(t, out)
	if len(m.pending) != 1 || m.pending[0] != "real task" {
		t.Errorf("real task should be unaffected, got %v", m.pending)
	}
}

func TestMemoryCapability_Execute_AddObservation_RingEvictsOldest(t *testing.T) {
	// The ring must drop the oldest entries, not the newest.
	m := NewMemoryCapability()
	for i := 0; i < maxObservations; i++ {
		m.Execute(context.Background(), fmt.Sprintf(`{"action":"add_observation","content":"obs%d"}`, i))
	}
	// Add one more — "obs0" should be evicted.
	m.Execute(context.Background(), `{"action":"add_observation","content":"newest"}`)
	if len(m.observations) != maxObservations {
		t.Fatalf("ring length: got %d, want %d", len(m.observations), maxObservations)
	}
	if m.observations[0] == "obs0" {
		t.Error("oldest entry obs0 should have been evicted")
	}
	if m.observations[maxObservations-1] != "newest" {
		t.Errorf("newest entry should be last, got %q", m.observations[maxObservations-1])
	}
}

func TestMemoryCapability_Execute_UpdatePlan_ClearsWithEmpty(t *testing.T) {
	m := NewMemoryCapability()
	m.Execute(context.Background(), `{"action":"update_plan","plan":"old plan"}`)
	m.Execute(context.Background(), `{"action":"update_plan","plan":""}`)
	if m.plan != "" {
		t.Errorf("expected plan cleared to empty, got %q", m.plan)
	}
}

func TestMemoryCapability_BuildMemorySection_NoneForEmptySubfields(t *testing.T) {
	// When only a plan is set, the other subsections should show "(none)".
	m := NewMemoryCapability()
	m.Execute(context.Background(), `{"action":"update_plan","plan":"just a plan"}`)
	sec := m.BuildMemorySection()
	if !strings.Contains(sec, "(none)") {
		t.Errorf("expected (none) placeholder for empty subsections, got:\n%s", sec)
	}
}

func TestMemoryCapability_ConcurrentAccess(t *testing.T) {
	m := NewMemoryCapability()
	const goroutines = 20
	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			m.Execute(context.Background(), `{"action":"add_observation","content":"concurrent"}`)
			m.Execute(context.Background(), `{"action":"add_pending","task":"concurrent task"}`)
			m.Execute(context.Background(), `{"action":"complete_task","task":"concurrent task"}`)
			m.BuildMemorySection()
			done <- struct{}{}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// assertOK checks that Execute returned {"ok":true}.
func assertOK(t *testing.T, out string) {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v (got %q)", err, out)
	}
	if result["ok"] != true {
		t.Errorf("expected {\"ok\":true}, got %q", out)
	}
}
