package acp

import "context"

// terminalCaller is the subset of the client API a [TerminalHandle] needs.
type terminalCaller interface {
	TerminalOutput(ctx context.Context, params *TerminalOutputRequest) (*TerminalOutputResponse, error)
	WaitForTerminalExit(ctx context.Context, params *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error)
	KillTerminal(ctx context.Context, params *KillTerminalRequest) (*KillTerminalResponse, error)
	ReleaseTerminal(ctx context.Context, params *ReleaseTerminalRequest) (*ReleaseTerminalResponse, error)
}

// TerminalHandle binds a terminal id to its session so an agent can poll,
// wait, kill and release without repeating both ids.
//
// Always Release a terminal when done; the client keeps the process and its
// buffered output alive until then.
//
// See protocol docs: [Terminals](https://agentclientprotocol.com/protocol/terminals)
type TerminalHandle struct {
	ID        TerminalID
	sessionID SessionID
	client    terminalCaller
}

// NewTerminalHandle binds an existing terminal id to the client that owns it.
// [AgentSideConnection.NewTerminal] creates one directly.
func NewTerminalHandle(id TerminalID, sessionID SessionID, client terminalCaller) *TerminalHandle {
	return &TerminalHandle{ID: id, sessionID: sessionID, client: client}
}

// CurrentOutput returns the output so far without waiting for exit.
func (t *TerminalHandle) CurrentOutput(ctx context.Context) (*TerminalOutputResponse, error) {
	return t.client.TerminalOutput(ctx, &TerminalOutputRequest{
		SessionID:  t.sessionID,
		TerminalID: t.ID,
	})
}

// WaitForExit blocks until the command exits and reports its status.
func (t *TerminalHandle) WaitForExit(ctx context.Context) (*WaitForTerminalExitResponse, error) {
	return t.client.WaitForTerminalExit(ctx, &WaitForTerminalExitRequest{
		SessionID:  t.sessionID,
		TerminalID: t.ID,
	})
}

// Kill stops the command but keeps the terminal id valid, so the final output
// and exit status remain readable.
func (t *TerminalHandle) Kill(ctx context.Context) error {
	_, err := t.client.KillTerminal(ctx, &KillTerminalRequest{
		SessionID:  t.sessionID,
		TerminalID: t.ID,
	})
	return err
}

// Release kills the command if it is still running and frees the terminal.
// The id is invalid afterwards, though tool calls that already reference it
// keep displaying its output.
func (t *TerminalHandle) Release(ctx context.Context) error {
	_, err := t.client.ReleaseTerminal(ctx, &ReleaseTerminalRequest{
		SessionID:  t.sessionID,
		TerminalID: t.ID,
	})
	return err
}
