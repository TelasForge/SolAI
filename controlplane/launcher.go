package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"

	"github.com/melodyogonna/solai/session"
)

// Launcher starts a session runtime. This is the sandbox seam: the control
// plane only launches a command. To isolate sessions, the operator points the
// command at their own infrastructure (e.g. a microVM wrapper).
type Launcher interface {
	// Start begins a runtime for spec and returns its process. The spec is
	// delivered on stdin; events are read from Events().
	Start(ctx context.Context, spec session.Spec) (RuntimeProcess, error)
}

// RuntimeProcess is a running session runtime.
type RuntimeProcess interface {
	// Events returns the JSON-lines event stream. Reading until EOF signals
	// that the process has finished writing.
	Events() io.Reader

	// Stop terminates the process.
	Stop() error

	// Wait blocks until the process exits and returns its exit error.
	Wait() error
}

// ExecLauncher runs the runtime as a shell command. The session spec is passed
// on stdin and events are read from stdout. Command is the entire command
// string, so it can be a sandbox wrapper such as
// "msb run -- solai".
type ExecLauncher struct {
	Command string
	Stderr  io.Writer // optional; where the runtime's logs go
}

// Start implements Launcher.
func (l ExecLauncher) Start(ctx context.Context, spec session.Spec) (RuntimeProcess, error) {
	payload, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", l.Command)
	cmd.Stdin = bytes.NewReader(payload)
	if l.Stderr != nil {
		cmd.Stderr = l.Stderr
	}

	pr, pw := io.Pipe()
	cmd.Stdout = pw

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		return nil, err
	}

	p := &execProc{cmd: cmd, events: pr, done: make(chan struct{})}
	go func() {
		p.waitErr = cmd.Wait()
		_ = pw.CloseWithError(p.waitErr)
		close(p.done)
	}()
	return p, nil
}

type execProc struct {
	cmd     *exec.Cmd
	events  io.ReadCloser
	done    chan struct{}
	waitErr error
}

func (p *execProc) Events() io.Reader { return p.events }

func (p *execProc) Stop() error {
	if p.cmd.Process != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}

func (p *execProc) Wait() error {
	<-p.done
	return p.waitErr
}
