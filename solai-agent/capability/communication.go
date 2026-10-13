package capability

import (
	"context"
	"fmt"
	"os"
)

// CommunicationCapability is a Core capability that manages per-invocation
// temporary directories for agentic tool IPC.
//
// Before starting a tool, the coordinator calls Allocate to create a fresh IPC
// directory, writes input.json into it, and injects SOLAI_IPC_DIR pointing at
// the directory. After the tool process exits, the coordinator reads output.json
// from the directory and calls Release to clean up.
type CommunicationCapability struct{}

// NewCommunicationCapability creates a CommunicationCapability.
func NewCommunicationCapability() *CommunicationCapability {
	return &CommunicationCapability{}
}

func (c *CommunicationCapability) Name() string                   { return "communication" }
func (c *CommunicationCapability) Class() CapabilityClass         { return Core }
func (c *CommunicationCapability) Description() string            { return "" }
func (c *CommunicationCapability) ToolRequestDescription() string { return "" }

// Execute is not used for Core capabilities.
func (c *CommunicationCapability) Execute(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("communication capability is not callable")
}

// Allocate creates a fresh temporary directory for a single tool invocation.
// The caller must call Release when the invocation is complete.
func (c *CommunicationCapability) Allocate() (string, error) {
	return os.MkdirTemp("", "solai-ipc-*")
}

// Release removes the IPC directory and all its contents.
func (c *CommunicationCapability) Release(dir string) {
	os.RemoveAll(dir)
}
