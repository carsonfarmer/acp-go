package acpv1

import (
	"context"
	"iter"
	"strings"

	"github.com/ironpark/go-acp/internal/acpconn"
	schema "github.com/ironpark/go-acp/schema/v1"
)

// ClientSession drives prompt turns on one session from the client side:
//
//	session, err := agent.StartSession(ctx, &acpv1.NewSessionRequest{Cwd: cwd})
//	turn, err := session.Prompt(ctx, acpv1.TextBlock("Summarize README.md"))
//	for update := range turn.Updates() {
//		// render tool calls, plans, message chunks...
//	}
//	response, err := turn.Wait()
//
// The client's [Client.SessionUpdate] still receives every update; a turn
// sees a copy of those that arrive while it runs.
type ClientSession struct {
	ID   SessionID
	conn *ClientSideConnection
}

// StartSession creates a session and returns a handle for prompting it. Use
// [ClientSideConnection.NewSession] instead when the response's modes or
// config options are needed, then [ClientSideConnection.Session].
func (c *ClientSideConnection) StartSession(ctx context.Context, params *NewSessionRequest) (*ClientSession, error) {
	response, err := c.NewSession(ctx, params)
	if err != nil {
		return nil, err
	}
	return c.Session(response.SessionID), nil
}

// Session returns a handle for a session the connection already knows, such
// as one created with NewSession or restored with LoadSession.
func (c *ClientSideConnection) Session(id SessionID) *ClientSession {
	return &ClientSession{ID: id, conn: c}
}

// Prompt starts a turn and returns at once; the turn ends when the agent
// answers the prompt. A v1 session runs one turn at a time, so Prompt fails
// with [acp.ErrTurnInProgress] until the previous turn has ended. Cancelling ctx abandons the request; use
// [ClientSession.Cancel] to stop the turn the way the protocol intends.
func (s *ClientSession) Prompt(ctx context.Context, content ...ContentBlock) (*Turn, error) {
	t, err := s.conn.turns.Begin(s.ID)
	if err != nil {
		return nil, err
	}
	go func() {
		response, err := s.conn.Prompt(ctx, &PromptRequest{SessionID: s.ID, Prompt: content})
		s.conn.turns.End(s.ID, t, response, err)
	}()
	return &Turn{t: t}, nil
}

// Cancel asks the agent to stop the session's current turn; the turn then
// ends with [schema.StopReasonCancelled].
func (s *ClientSession) Cancel(ctx context.Context) error {
	return s.conn.Cancel(ctx, &CancelNotification{SessionID: s.ID})
}

// Turn is one prompt turn started by [ClientSession.Prompt].
type Turn struct {
	t *acpconn.Turn[SessionUpdate, *PromptResponse]
}

// Updates yields the turn's session updates in order and stops when the turn
// ends. Updates that arrived before the call are included. Only one reader
// should range over it.
func (t *Turn) Updates() iter.Seq[SessionUpdate] { return t.t.Updates() }

// Wait blocks until the turn ends and returns the agent's response.
func (t *Turn) Wait() (*PromptResponse, error) { return t.t.Wait() }

// Done is closed once the turn ends.
func (t *Turn) Done() <-chan struct{} { return t.t.Done() }

// Text consumes the turn's updates and returns the agent's message text, for
// callers that only want the answer. It reports the same error as Wait.
func (t *Turn) Text() (string, error) {
	var b strings.Builder
	for update := range t.Updates() {
		if chunk, ok := update.Variant().(schema.SessionUpdateAgentMessageChunk); ok {
			if text, ok := TextOf(chunk.Content); ok {
				b.WriteString(text)
			}
		}
	}
	_, err := t.Wait()
	return b.String(), err
}
