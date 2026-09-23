package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ironpark/acp-go/acp1"
)

// runTurn plans the turn, then works through it: a command in the client's
// terminal, a file read, and a file edit that needs permission in ask mode.
// Each plan update replaces the last, so the agent resends the whole plan as
// each step finishes and the next starts.
//
// A tool call id must be unique within the session, across turns, so each
// tool call gets a generated one.
func (a *exampleAgent) runTurn(ctx context.Context, sessionID acp1.SessionID, sess *session, prompt string) error {
	stream := acp1.NewSessionStream(a.client, sessionID)
	plan := []acp1.PlanEntry{
		{Content: "Check the Go toolchain", Priority: acp1.PlanEntryPriorityMedium, Status: acp1.PlanEntryStatusInProgress},
		{Content: "Read the project", Priority: acp1.PlanEntryPriorityMedium, Status: acp1.PlanEntryStatusPending},
		{Content: "Update the configuration", Priority: acp1.PlanEntryPriorityHigh, Status: acp1.PlanEntryStatusPending},
	}
	// step runs entry i, then marks it completed and the next in progress.
	step := func(i int, run func() error) error {
		if err := run(); err != nil {
			return err
		}
		plan[i].Status = acp1.PlanEntryStatusCompleted
		if i+1 < len(plan) {
			plan[i+1].Status = acp1.PlanEntryStatusInProgress
		}
		return stream.SendPlan(ctx, plan)
	}

	if err := stream.SendText(ctx, fmt.Sprintf("You said %q. Here is my plan.", prompt)); err != nil {
		return err
	}
	if err := stream.SendPlan(ctx, plan); err != nil {
		return err
	}
	if err := step(0, func() error { return a.runCommand(ctx, stream, sess, "go", "version") }); err != nil {
		return err
	}
	if err := step(1, func() error { return readProject(ctx, stream) }); err != nil {
		return err
	}
	return step(2, func() error { return a.editConfig(ctx, stream, sess) })
}

// runCommand runs a command in a terminal the client owns and shows its
// output in the tool call as it runs. Clients without the terminal
// capability cannot, so the agent says so instead.
func (a *exampleAgent) runCommand(ctx context.Context, stream *acp1.SessionStream, sess *session, command string, args ...string) error {
	id := acp1.GenerateToolCallID()
	title := "Running " + strings.Join(append([]string{command}, args...), " ")
	if err := stream.StartToolCall(ctx, id, title, acp1.ToolKindExecute); err != nil {
		return err
	}
	if !a.terminal {
		return stream.CompleteToolCall(ctx, id, acp1.WithToolContent(acp1.ToolText("This client cannot run commands.")))
	}

	terminal, err := a.client.NewTerminal(ctx, &acp1.CreateTerminalRequest{
		SessionID: stream.SessionID(),
		Command:   command,
		Args:      args,
		Cwd:       &sess.cwd,
	})
	if err != nil {
		return stream.FailToolCall(ctx, id, acp1.WithToolContent(acp1.ToolText(err.Error())))
	}
	// Release frees the terminal; the client still shows the output of the
	// tool calls that embed it.
	defer terminal.Release(context.WithoutCancel(ctx))

	exit, err := terminal.WaitForExit(ctx)
	if err != nil {
		_ = terminal.Kill(context.WithoutCancel(ctx)) // the turn was cancelled
		return err
	}
	// No exit code means a signal ended it, so ExitCode is checked for nil
	// rather than read with GetExitCode, which would give 0.
	if exit.ExitCode == nil || *exit.ExitCode != 0 {
		return stream.FailToolCall(ctx, id, acp1.WithToolContent(acp1.ToolTerminal(terminal.ID)))
	}
	return stream.CompleteToolCall(ctx, id, acp1.WithToolContent(acp1.ToolTerminal(terminal.ID)))
}

func readProject(ctx context.Context, stream *acp1.SessionStream) error {
	id := acp1.GenerateToolCallID()
	if err := stream.StartToolCall(ctx, id, "Reading project files", acp1.ToolKindRead); err != nil {
		return err
	}
	if err := pause(ctx); err != nil {
		return err
	}
	return stream.CompleteToolCall(ctx, id, acp1.WithToolContent(acp1.ToolText("# My Project")))
}

// editConfig proposes a change to config.json and reports it as a diff. In
// ask mode it asks the user first. The example does not write the file.
func (a *exampleAgent) editConfig(ctx context.Context, stream *acp1.SessionStream, sess *session) error {
	id := acp1.GenerateToolCallID()
	path := filepath.Join(sess.cwd, "config.json")
	if err := stream.StartToolCall(ctx, id, "Modifying configuration", acp1.ToolKindEdit, acp1.WithLocations(acp1.ToolCallLocation{Path: path})); err != nil {
		return err
	}
	oldText, newText := "{\"debug\": false}\n", "{\"debug\": true}\n"
	diff := acp1.ToolDiff(path, &oldText, newText)

	if sess.currentMode() == askMode {
		allowed, err := a.askPermission(ctx, stream.SessionID(), id, path, diff)
		if err != nil {
			return err
		}
		if !allowed {
			if err := stream.FailToolCall(ctx, id); err != nil {
				return err
			}
			return stream.SendText(ctx, " Skipping the configuration update.")
		}
	}
	if err := stream.CompleteToolCall(ctx, id, acp1.WithToolContent(diff)); err != nil {
		return err
	}
	return stream.SendText(ctx, " Configuration updated.")
}

// askPermission shows the user the proposed diff and asks whether to apply it.
func (a *exampleAgent) askPermission(ctx context.Context, sessionID acp1.SessionID, id acp1.ToolCallID, path string, diff acp1.ToolCallContent) (bool, error) {
	permission, err := a.client.RequestPermission(ctx, &acp1.RequestPermissionRequest{
		SessionID: sessionID,
		ToolCall: acp1.ToolCallUpdate{
			ToolCallID: id,
			Title:      new("Modifying configuration"),
			Kind:       new(acp1.ToolKindEdit),
			Status:     new(acp1.ToolCallStatusPending),
			Locations:  []acp1.ToolCallLocation{{Path: path}},
			Content:    []acp1.ToolCallContent{diff},
		},
		Options: []acp1.PermissionOption{
			{OptionID: "allow", Name: "Allow this change", Kind: acp1.PermissionOptionKindAllowOnce},
			{OptionID: "reject", Name: "Skip this change", Kind: acp1.PermissionOptionKindRejectOnce},
		},
	})
	if err != nil {
		return false, err
	}
	// Anything but a selected "allow", including a cancelled request or an
	// outcome added after this example was written, skips the change.
	selected, ok := permission.Outcome.As[acp1.RequestPermissionOutcomeSelected]()
	return ok && selected.OptionID == "allow", nil
}

// pause stands in for real work and returns early when the turn is cancelled.
func pause(ctx context.Context) error {
	select {
	case <-time.After(500 * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
