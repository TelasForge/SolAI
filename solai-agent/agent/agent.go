package agent

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/melodyogonna/solai/llm"
	"github.com/melodyogonna/solai/ratelimit"
	"github.com/melodyogonna/solai/solai-agent/capability"
)

// cycleRetry is the retry strategy used for each agent cycle.
// It retries up to 3 times with exponential backoff starting at 2s, capped at 30s.
var cycleRetry = ratelimit.NewExponentialRetry(3, 2*time.Second, 30*time.Second, 2.0)

// maxCycleIterations bounds tool-calling rounds within a single cycle.
const maxCycleIterations = 10

// historyUserTurns is how many prior user turns are carried between cycles.
// The cut always lands on a user-message boundary so tool calls and their
// results are never split.
const historyUserTurns = 3

// Run is the main entry point for the autonomous agent loop.
// It blocks until ctx is cancelled, running one agent cycle per CycleInterval.
//
// On startup it loads agentic tools from cfg.ToolsDir, then enters a loop where
// each iteration runs a full native tool-calling cycle using the configured LLM.
func Run(ctx context.Context, cfg Config, capManager *capability.CapabilityManager) {
	warnings, err := cfg.SystemManager.Setup(capManager)
	if err != nil {
		slog.Error("system manager setup failed", "err", err)
		return
	}
	for _, w := range warnings {
		slog.Warn("tool setup warning", "err", w)
	}
	go cfg.SystemManager.Start(ctx)

	agentTools := append(capManager.GetAgentTools(), cfg.SystemManager.GetTools()...)
	if len(agentTools) == 0 {
		slog.Warn("no tools loaded — agent will report it cannot accomplish goals")
	} else {
		slog.Info("tools loaded", "count", len(agentTools))
		for _, t := range agentTools {
			slog.Info("tool available", "name", t.Name())
		}
	}

	var history []llm.Message
	var cycleNum int
	for {
		select {
		case <-ctx.Done():
			slog.Info("agent shutting down")
			return
		default:
		}

		cycleNum++
		slog.Info("starting agent cycle", "cycle", cycleNum)
		cycleCtx, cancel := context.WithTimeout(ctx, cfg.CycleTimeout)
		cyclePrompt := buildCyclePrompt(cfg, capManager)
		slog.Debug("cycle prompt", "cycle", cycleNum, "prompt", cyclePrompt)
		answer, newHistory, err := runCycle(cycleCtx, cfg, agentTools, cyclePrompt, history)
		cancel()

		if err != nil {
			handleCycleError(cycleNum, err)
		} else {
			history = llm.TrimHistory(newHistory, historyUserTurns)
			slog.Info("cycle complete", "cycle", cycleNum, "answer", answer)
		}
	}
}

// runCycle executes one complete tool-calling cycle with exponential retry on
// transient errors. Non-transient errors (max iterations, context
// cancellation/timeout) are not retried.
func runCycle(ctx context.Context, cfg Config, agentTools []llm.Tool, prompt string, history []llm.Message) (string, []llm.Message, error) {
	var (
		answer     string
		newHistory []llm.Message
	)
	attempt := 0
	err := cycleRetry.Execute(ctx, func(ctx context.Context) error {
		attempt++
		if attempt > 1 {
			slog.Info("retrying cycle", "attempt", attempt)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a := llm.NewAgent(cfg.LLM, agentTools, cfg.SystemPrompt,
			llm.WithMaxIterations(maxCycleIterations))
		var err error
		answer, newHistory, err = a.Run(ctx, prompt, history)
		if errors.Is(err, llm.ErrMaxIterations) {
			// Not a transient error — stop retrying immediately.
			return &noRetryError{err}
		}
		return err
	})
	var nre *noRetryError
	if errors.As(err, &nre) {
		return "", newHistory, nre.err
	}
	return answer, newHistory, err
}

// noRetryError wraps an error to signal that cycleRetry should not attempt further retries.
type noRetryError struct{ err error }

func (e *noRetryError) Error() string { return e.err.Error() }

// buildCyclePrompt assembles the user message passed to the agent each cycle.
// Tool definitions are advertised natively via the function-calling API, so
// the prompt only carries dynamic state: any MemorySectionProvider sections
// (e.g. MemoryCapability) and the user's goals.
func buildCyclePrompt(cfg Config, capManager *capability.CapabilityManager) string {
	var sections []string

	for _, c := range capManager.GetAll() {
		if mp, ok := c.(capability.MemorySectionProvider); ok {
			if sec := mp.BuildMemorySection(); sec != "" {
				sections = append(sections, sec)
			}
		}
	}

	sections = append(sections, "## Your Goals\n"+cfg.UserGoals)
	return strings.Join(sections, "\n\n")
}

// handleCycleError logs cycle errors with appropriate severity.
// The agent loop always continues after an error — it never crashes.
func handleCycleError(cycleNum int, err error) {
	switch {
	case errors.Is(err, llm.ErrMaxIterations):
		slog.Warn("cycle did not finish within max iterations — tools may be insufficient for the specified goals", "cycle", cycleNum)
	case errors.Is(err, context.DeadlineExceeded):
		slog.Warn("cycle timed out", "cycle", cycleNum)
	default:
		slog.Error("unexpected cycle error", "cycle", cycleNum, "err", err)
	}
}
