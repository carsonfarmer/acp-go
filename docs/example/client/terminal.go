package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"unicode/utf8"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acp1"
)

// terminals runs the commands an agent asks for, implementing the
// terminal/* methods of acp1.TerminalHandler. A real client would ask the
// user first, or run commands in a sandbox.
type terminals struct {
	mu   sync.Mutex
	next int
	byID map[acp1.TerminalID]*terminal
}

// terminal is one command and its output, kept after release so tool calls
// that embed it can still show it.
type terminal struct {
	cmd  *exec.Cmd
	done chan struct{} // closed once the command exits

	mu        sync.Mutex
	output    []byte
	limit     int // bytes kept, the end of the output; 0 keeps everything
	truncated bool
	exit      *acp1.TerminalExitStatus
}

func (t *terminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.output = append(t.output, p...)
	if t.limit > 0 && len(t.output) > t.limit {
		// Keep the end, starting at a character boundary.
		cut := len(t.output) - t.limit
		for cut < len(t.output) && !utf8.RuneStart(t.output[cut]) {
			cut++
		}
		t.output = t.output[cut:]
		t.truncated = true
	}
	return len(p), nil
}

func (t *terminal) snapshot() (output string, truncated bool, exit *acp1.TerminalExitStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.output), t.truncated, t.exit
}

func (ts *terminals) CreateTerminal(_ context.Context, params *acp1.CreateTerminalRequest) (*acp1.CreateTerminalResponse, error) {
	t := &terminal{cmd: exec.Command(params.Command, params.Args...), done: make(chan struct{})}
	if params.OutputByteLimit != nil {
		t.limit = int(*params.OutputByteLimit)
	}
	if params.Cwd != nil {
		t.cmd.Dir = *params.Cwd
	}
	t.cmd.Env = os.Environ()
	for _, v := range params.Env {
		t.cmd.Env = append(t.cmd.Env, v.Name+"="+v.Value)
	}
	t.cmd.Stdout, t.cmd.Stderr = t, t
	if err := t.cmd.Start(); err != nil {
		return nil, acp.ErrInternalError(err.Error())
	}
	go func() {
		_ = t.cmd.Wait()
		exit := &acp1.TerminalExitStatus{}
		if code := t.cmd.ProcessState.ExitCode(); code >= 0 {
			exit.ExitCode = new(uint32(code))
		} else {
			exit.Signal = new("killed")
		}
		t.mu.Lock()
		t.exit = exit
		t.mu.Unlock()
		close(t.done)
	}()

	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.next++
	id := acp1.TerminalID(fmt.Sprintf("term_%d", ts.next))
	if ts.byID == nil {
		ts.byID = map[acp1.TerminalID]*terminal{}
	}
	ts.byID[id] = t
	return &acp1.CreateTerminalResponse{TerminalID: id}, nil
}

func (ts *terminals) get(id acp1.TerminalID) (*terminal, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if t, ok := ts.byID[id]; ok {
		return t, nil
	}
	return nil, acp.ErrResourceNotFound(fmt.Sprintf("terminal %s", id))
}

func (ts *terminals) TerminalOutput(_ context.Context, params *acp1.TerminalOutputRequest) (*acp1.TerminalOutputResponse, error) {
	t, err := ts.get(params.TerminalID)
	if err != nil {
		return nil, err
	}
	output, truncated, exit := t.snapshot()
	return &acp1.TerminalOutputResponse{Output: output, Truncated: truncated, ExitStatus: exit}, nil
}

func (ts *terminals) WaitForTerminalExit(ctx context.Context, params *acp1.WaitForTerminalExitRequest) (*acp1.WaitForTerminalExitResponse, error) {
	t, err := ts.get(params.TerminalID)
	if err != nil {
		return nil, err
	}
	select {
	case <-t.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	_, _, exit := t.snapshot()
	return &acp1.WaitForTerminalExitResponse{ExitCode: exit.ExitCode, Signal: exit.Signal}, nil
}

// KillTerminal stops the command; its output and exit status stay readable.
func (ts *terminals) KillTerminal(_ context.Context, params *acp1.KillTerminalRequest) (*acp1.KillTerminalResponse, error) {
	t, err := ts.get(params.TerminalID)
	if err != nil {
		return nil, err
	}
	select {
	case <-t.done:
	default:
		_ = t.cmd.Process.Kill()
	}
	return &acp1.KillTerminalResponse{}, nil
}

// ReleaseTerminal kills the command if it still runs. This client keeps the
// output to show in the tool calls that embed it; a long-running client
// would drop it once no tool call on screen does.
func (ts *terminals) ReleaseTerminal(ctx context.Context, params *acp1.ReleaseTerminalRequest) (*acp1.ReleaseTerminalResponse, error) {
	if _, err := ts.KillTerminal(ctx, &acp1.KillTerminalRequest{SessionID: params.SessionID, TerminalID: params.TerminalID}); err != nil {
		return nil, err
	}
	return &acp1.ReleaseTerminalResponse{}, nil
}
