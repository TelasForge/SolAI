package capability

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/melodyogonna/solai/llm"
)

// ToolLoaderFunc loads agentic tools. Injected into SystemManager to avoid
// circular imports between capability and tool packages.
//
// capManager provides capability validation and is passed through to each
// loaded tool so it can dispatch capability requests at runtime.
type ToolLoaderFunc func(capManager *CapabilityManager) ([]llm.Tool, []error, error)

// CleanupJob is a periodic administrative task run by SystemManager in the background.
type CleanupJob struct {
	Name     string
	Interval time.Duration
	Fn       func(ctx context.Context) error
}

// SystemManager is a Core capability that owns the agent's operational environment:
// tool loading, LLM provider logging, and cleanup job scheduling.
type SystemManager struct {
	loader   ToolLoaderFunc
	provider *LLMProvider
	tools    []llm.Tool
	jobs     []CleanupJob
	ready    bool
}

// NewSystemManager creates a SystemManager with the given tool loader and LLM provider.
// The provider is used only for startup logging in Setup.
// Call Setup before GetTools, and optionally RegisterJob before Start.
func NewSystemManager(loader ToolLoaderFunc, provider *LLMProvider) *SystemManager {
	return &SystemManager{loader: loader, provider: provider}
}

// Name implements Capability.
func (m *SystemManager) Name() string { return "system-manager" }

// Class implements Capability — Core class, never injected into prompts.
func (m *SystemManager) Class() CapabilityClass { return Core }

// Description implements Capability — empty because Core capabilities are invisible.
func (m *SystemManager) Description() string { return "" }

// ToolRequestDescription implements Capability — Core capabilities are not requestable.
func (m *SystemManager) ToolRequestDescription() string { return "" }

// Execute returns a JSON status report of loaded tools and registered jobs.
// Useful for future diagnostic tooling; safe to call before or after Setup.
func (m *SystemManager) Execute(_ context.Context, _ string) (string, error) {
	toolNames := make([]string, len(m.tools))
	for i, t := range m.tools {
		toolNames[i] = t.Name()
	}

	jobNames := make([]string, len(m.jobs))
	for i, j := range m.jobs {
		jobNames[i] = j.Name
	}

	status := map[string]any{
		"tools_loaded":    len(m.tools),
		"tools":           toolNames,
		"jobs_registered": len(m.jobs),
		"jobs":            jobNames,
	}
	data, err := json.Marshal(status)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Setup prepares the agent's operational environment:
//  1. Logs configured LLM providers.
//  2. Loads tools via the injected loader.
//
// capManager is passed to the tool loader for capability validation and runtime
// request dispatch. It must not be nil.
//
// Returns (warnings, fatal_error). Must be called before GetTools.
func (m *SystemManager) Setup(capManager *CapabilityManager) ([]error, error) {
	if providers := m.provider.ConfiguredProviders(); len(providers) > 0 {
		slog.Info("LLM provider configured", "providers", providers)
	} else {
		slog.Warn("no OpenRouter API key configured — tools requiring an LLM will be disabled")
	}

	tools, warnings, err := m.loader(capManager)
	if err != nil {
		return nil, err
	}
	m.tools = tools
	m.ready = true
	return warnings, nil
}

// GetTools returns tools loaded by Setup. Panics if Setup was not called first.
func (m *SystemManager) GetTools() []llm.Tool {
	if !m.ready {
		panic("system_manager: GetTools called before Setup")
	}
	return m.tools
}

// RegisterJob appends a cleanup job. Must be called before Start.
func (m *SystemManager) RegisterJob(job CleanupJob) {
	m.jobs = append(m.jobs, job)
}

// Start launches one goroutine per registered job and blocks until ctx is cancelled,
// then waits for all goroutines to exit. Designed to be called as go sm.Start(ctx).
func (m *SystemManager) Start(ctx context.Context) {
	var wg sync.WaitGroup
	for _, job := range m.jobs {
		wg.Add(1)
		go func(j CleanupJob) {
			defer wg.Done()
			m.runJob(ctx, j)
		}(job)
	}

	// Wait for ctx cancellation so Start only returns once all jobs have stopped.
	<-ctx.Done()
	wg.Wait()
}

// runJob runs a single cleanup job on its interval until ctx is cancelled.
// Errors from the job are logged as warnings and the job keeps running.
func (m *SystemManager) runJob(ctx context.Context, job CleanupJob) {
	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := job.Fn(ctx); err != nil {
				slog.Warn("cleanup job error", "job", job.Name, "err", err)
			}
		}
	}
}
