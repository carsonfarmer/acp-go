package acpconn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// Conn is the lifecycle surface both façades' connections share.
type Conn interface {
	Start(ctx context.Context) error
	Close() error
	Done() <-chan struct{}
}

// Spawn starts cmd, connects to its stdio through connect and starts the
// connection's read loop. The process is killed when ctx is done, and the
// connection is closed once the process exits.
//
// The returned wait blocks until both have stopped. It reports ctx's error if
// ctx ended the process, otherwise the process's exit error, otherwise the
// read loop's error.
func Spawn(ctx context.Context, cmd *exec.Cmd, connect func(r io.Reader, w io.Writer) Conn) (wait func() error, err error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("agent stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("agent stdout pipe: %w", err)
	}
	if cmd.Stderr == nil {
		// An agent reports startup failures on stderr; dropping it would
		// leave the caller with nothing but a closed connection.
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start agent process: %w", err)
	}

	conn := connect(stdout, stdin)
	stopKill := context.AfterFunc(ctx, func() { _ = cmd.Process.Kill() })
	// Closing the agent's stdin is how a stdio agent learns the client is
	// gone; it exits, and the read loop then sees EOF on its stdout.
	go func() {
		<-conn.Done()
		_ = stdin.Close()
	}()

	loopDone := make(chan error, 1)
	go func() { loopDone <- conn.Start(ctx) }()

	processDone := make(chan error, 1)
	go func() {
		// cmd.Wait returns once the process exits, so this goroutine always ends.
		err := cmd.Wait()
		stopKill()
		_ = conn.Close()
		processDone <- err
	}()

	return sync.OnceValue(func() error {
		loopErr, processErr := <-loopDone, <-processDone
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case processErr != nil:
			return fmt.Errorf("agent process: %w", processErr)
		case errors.Is(loopErr, context.Canceled):
			return nil // closed after the process exited cleanly
		}
		return loopErr
	}), nil
}

// Pipe connects two connections in memory and starts both read loops. When
// either side stops, both pipes close, so the other side reads EOF and stops.
func Pipe(ctx context.Context, agent, client func(r io.Reader, w io.Writer) Conn) {
	toAgentR, toAgentW := io.Pipe()
	toClientR, toClientW := io.Pipe()
	a := agent(toAgentR, toClientW)
	c := client(toClientR, toAgentW)
	closeAll := sync.OnceFunc(func() {
		for _, p := range []io.Closer{toAgentR, toAgentW, toClientR, toClientW} {
			_ = p.Close()
		}
	})
	for _, conn := range []Conn{a, c} {
		go func() { _ = conn.Start(ctx) }()
		go func() {
			<-conn.Done()
			closeAll()
		}()
	}
}
