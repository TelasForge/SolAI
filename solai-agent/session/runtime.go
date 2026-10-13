package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/melodyogonna/solai/llm"
)

// defaultSafetyCap bounds a session's model turns when Spec.SafetyCap is unset.
const defaultSafetyCap = 100

// ErrSafetyCap is returned when a session reaches its safety iteration cap.
// The session is paused, not failed; the caller may resume it with a new cap.
var ErrSafetyCap = errors.New("session: safety iteration cap reached")

const defaultSystemPrompt = `You are SolAI, an autonomous agent operating on the Solana blockchain.

You work through a list of goals one at a time. For the current goal, make
progress step by step using the available tools to gather data and perform
actions.

Goal completion is explicit: when the current goal is fully achieved, call the
"goal" tool with action "complete". If it cannot be achieved, call "fail"; if it
needs external input, call "block"; to record progress without ending the goal,
use "note". Never claim a goal is complete unless it truly is. Keep working
until the current goal is resolved.

The capabilities selected for this session are available to you as tools. A tool
may also ask the runtime to perform a capability on its behalf: when a tool
result contains a 'Capability "..." result:' line followed by an 'Instruction:',
the runtime has already performed it — follow the instruction, usually by
re-invoking the same tool with the result in its payload.`

const continuePrompt = `You stopped without resolving the current goal. Continue working on it, or call
the goal tool with action "complete", "fail", or "block" if appropriate. Do not
stop until the goal is resolved.`

// Runtime runs one session to completion.
type Runtime struct {
	spec   Spec
	client *llm.Client
	tools  []llm.Tool
	byName map[string]llm.Tool
	emit   Emitter

	systemPrompt string
	safetyCap    int

	goals   []*Goal
	current *Goal
	turns   int
}

// New builds a Runtime from a spec, an LLM client, the selected tools, and an
// event emitter. The goal-management tool is added automatically.
func New(spec Spec, client *llm.Client, tools []llm.Tool, emit Emitter) *Runtime {
	if emit == nil {
		emit = func(Event) {}
	}
	r := &Runtime{
		spec:         spec,
		client:       client,
		byName:       map[string]llm.Tool{},
		emit:         emit,
		systemPrompt: buildSystemPrompt(spec),
		safetyCap:    spec.SafetyCap,
	}
	if r.safetyCap <= 0 {
		r.safetyCap = defaultSafetyCap
	}
	for _, text := range spec.Goals {
		r.goals = append(r.goals, &Goal{
			ID:     fmt.Sprintf("g%d", len(r.goals)+1),
			Text:   text,
			Status: GoalPending,
		})
	}

	r.tools = make([]llm.Tool, 0, len(tools)+1)
	r.tools = append(r.tools, tools...)
	r.tools = append(r.tools, &goalTool{r: r})
	for _, t := range r.tools {
		r.byName[t.Name()] = t
	}
	return r
}

// Goals returns the session's goals and their current status.
func (r *Runtime) Goals() []Goal {
	out := make([]Goal, len(r.goals))
	for i, g := range r.goals {
		out[i] = *g
	}
	return out
}

// Run executes the session. It returns ErrSafetyCap if the safety cap was hit,
// or the underlying error on failure. A nil error means every goal reached a
// terminal state.
func (r *Runtime) Run(ctx context.Context) error {
	r.emit(Event{Type: EventSessionStarted, Status: string(StatusRunning)})

	var (
		history []llm.Message
		runErr  error
	)
	for {
		goal := r.nextPending()
		if goal == nil {
			break
		}
		r.current = goal
		goal.Status = GoalRunning
		r.emit(Event{Type: EventGoalStarted, GoalID: goal.ID, Message: goal.Text})

		if err := r.runGoal(ctx, goal, &history); err != nil {
			runErr = err
			break
		}
	}

	r.emit(Event{Type: EventSessionDone, Status: string(r.finalStatus(runErr))})
	return runErr
}

// runGoal drives a single goal until it reaches a terminal state or an error
// occurs. The model is re-prompted to continue whenever it stops without
// resolving the goal, so a long task is never cut short by a plain final answer.
func (r *Runtime) runGoal(ctx context.Context, goal *Goal, history *[]llm.Message) error {
	*history = append(*history, llm.Message{Role: llm.RoleUser, Content: goalPrompt(goal)})

	for goal.Status == GoalRunning {
		if r.turns >= r.safetyCap {
			r.emit(Event{Type: EventError, Message: ErrSafetyCap.Error()})
			return ErrSafetyCap
		}
		reply, err := r.step(ctx, history)
		if err != nil {
			return err
		}
		if goal.Status != GoalRunning {
			break
		}
		if len(reply.ToolCalls) == 0 {
			// The model produced a final answer without resolving the goal.
			// Re-prompt instead of ending the session.
			*history = append(*history, llm.Message{Role: llm.RoleUser, Content: continuePrompt})
		}
	}

	if goal.Status != GoalRunning {
		r.emit(Event{Type: EventGoalDone, GoalID: goal.ID, Status: string(goal.Status), Message: goal.Note})
	}
	return nil
}

// step performs one model call, executes any requested tool calls, appends the
// results to history, and emits the corresponding events.
func (r *Runtime) step(ctx context.Context, history *[]llm.Message) (*llm.Message, error) {
	r.turns++

	msgs := make([]llm.Message, 0, len(*history)+1)
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: r.systemPrompt})
	msgs = append(msgs, *history...)

	reply, err := r.client.Chat(ctx, msgs, r.tools)
	if err != nil {
		r.emit(Event{Type: EventError, GoalID: r.goalID(), Message: err.Error()})
		return nil, err
	}

	*history = append(*history, *reply)
	if reply.Content != "" {
		r.emit(Event{Type: EventMessage, GoalID: r.goalID(), Message: reply.Content})
	}

	for _, call := range reply.ToolCalls {
		r.emit(Event{Type: EventToolCall, GoalID: r.goalID(), Tool: call.Function.Name, Args: call.Function.Arguments})
		content := r.execTool(ctx, call)
		r.emit(Event{Type: EventToolResult, GoalID: r.goalID(), Tool: call.Function.Name, Result: content})
		*history = append(*history, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: call.ID,
			Content:    content,
		})
	}
	return reply, nil
}

// execTool runs a single tool call, rendering errors as text so the model can
// observe and recover from them.
func (r *Runtime) execTool(ctx context.Context, call llm.ToolCall) string {
	tool, ok := r.byName[call.Function.Name]
	if !ok {
		return fmt.Sprintf("Error: unknown tool %q", call.Function.Name)
	}
	args := call.Function.Arguments
	if args == "" {
		args = "{}"
	}
	out, err := tool.Call(ctx, args)
	if err != nil {
		if out != "" {
			return out + "\nError: " + err.Error()
		}
		return "Error: " + err.Error()
	}
	return out
}

func (r *Runtime) nextPending() *Goal {
	for _, g := range r.goals {
		if g.Status == GoalPending {
			return g
		}
	}
	return nil
}

func (r *Runtime) goalID() string {
	if r.current == nil {
		return ""
	}
	return r.current.ID
}

func (r *Runtime) finalStatus(runErr error) Status {
	switch {
	case errors.Is(runErr, ErrSafetyCap):
		return StatusPaused
	case runErr != nil:
		return StatusFailed
	}
	status := StatusDone
	for _, g := range r.goals {
		switch g.Status {
		case GoalFailed:
			status = StatusFailed
		case GoalBlocked:
			status = StatusPaused
		}
	}
	return status
}

func buildSystemPrompt(spec Spec) string {
	if spec.SystemPrompt != "" {
		return spec.SystemPrompt
	}
	return defaultSystemPrompt
}

func goalPrompt(g *Goal) string {
	return fmt.Sprintf("Current goal: %s\n\nWork on this goal now, using tools as needed. "+
		"When it is finished, call the goal tool with action \"complete\".", g.Text)
}

// goalTool lets the model explicitly resolve the current goal. It is injected
// by the runtime and is not part of the user-selectable capability set.
type goalTool struct{ r *Runtime }

func (t *goalTool) Name() string { return "goal" }

func (t *goalTool) Description() string {
	return `Resolve or annotate the current goal. action is one of:
  "complete" — the goal is fully achieved
  "fail"     — the goal cannot be achieved
  "block"    — the goal needs external input before it can continue
  "note"     — record progress without ending the goal
Provide "note" with a short explanation when relevant.`
}

func (t *goalTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{"complete", "fail", "block", "note"},
			},
			"note": map[string]any{"type": "string"},
		},
		"required": []string{"action"},
	}
}

func (t *goalTool) Call(_ context.Context, argsJSON string) (string, error) {
	action := llm.ArgString(argsJSON, "action")
	note := llm.ArgString(argsJSON, "note")

	g := t.r.current
	if g == nil {
		return `{"error":"no active goal"}`, nil
	}
	switch action {
	case "complete":
		g.Status = GoalDone
	case "fail":
		g.Status = GoalFailed
	case "block":
		g.Status = GoalBlocked
	case "note":
		// status unchanged
	default:
		return `{"error":"unknown action"}`, nil
	}
	if note != "" {
		g.Note = note
	}
	return `{"ok":true}`, nil
}
