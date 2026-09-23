//go:build unix

package acpconn

import (
	"context"
	"encoding/json/jsontext"
	"os/exec"
	"testing"
	"time"

	"github.com/ironpark/acp-go/internal/jsonrpc"
)

func spawnForTest(t *testing.T, cmd *exec.Cmd) (wait, stop func() error) {
	t.Helper()
	wait, stop, err := Spawn(t.Context(), cmd, func(tr jsonrpc.Transport) Conn {
		return jsonrpc.New(func(context.Context, string, jsontext.Value) (any, error) { return nil, nil },
			func(context.Context, string, jsontext.Value) error { return nil }, tr)
	})
	if err != nil {
		t.Fatal(err)
	}
	return wait, stop
}

// TestStopWaitsForTheProcess: an agent that exits when its stdin closes has
// exited by the time stop returns.
func TestStopWaitsForTheProcess(t *testing.T) {
	cmd := exec.Command("cat")
	_, stop := spawnForTest(t, cmd)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		t.Fatalf("stop returned before the process exited: %v", cmd.ProcessState)
	}
}

// TestStopKillsAStuckProcess: an agent that ignores its closed stdin is
// killed once the grace period ends.
func TestStopKillsAStuckProcess(t *testing.T) {
	defer func(grace time.Duration) { exitGrace = grace }(exitGrace)
	exitGrace = 50 * time.Millisecond
	cmd := exec.Command("sleep", "60")
	wait, stop := spawnForTest(t, cmd)
	start := time.Now()
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("stop took %v", elapsed)
	}
	if err := wait(); err == nil {
		t.Fatal("Wait reported a clean exit for a killed process")
	}
}
