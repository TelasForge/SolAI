package capability

import (
	"context"
	"encoding/json"
)

// NetworkManagerCapability is a Regular capability that records that a tool
// declares outbound network access via "network-manager" in its manifest.
//
// Tools run as ordinary subprocesses, so network access is always available at
// the OS level; the capability remains as a declarative marker and as a
// validation target for manifests' required_capabilities field.
type NetworkManagerCapability struct{}

// NewNetworkManagerCapability constructs a NetworkManagerCapability.
func NewNetworkManagerCapability() *NetworkManagerCapability {
	return &NetworkManagerCapability{}
}

// Name implements Capability.
func (n *NetworkManagerCapability) Name() string { return "network-manager" }

// Class implements Capability — Regular, so tools can declare it in their manifest.
func (n *NetworkManagerCapability) Class() CapabilityClass { return Regular }

// Description implements Capability — empty so it is not shown to the
// coordinator LLM as a callable tool. Network access is declared statically
// via required_capabilities; the LLM cannot influence this.
func (n *NetworkManagerCapability) Description() string { return "" }

// Execute implements Capability. Returns a simple status JSON.
func (n *NetworkManagerCapability) Execute(_ context.Context, _ string) (string, error) {
	out, _ := json.Marshal(map[string]string{"status": "network access granted"})
	return string(out), nil
}

// ToolRequestDescription implements Capability. Network access is declared via
// required_capabilities, not via runtime requests.
func (n *NetworkManagerCapability) ToolRequestDescription() string { return "" }
