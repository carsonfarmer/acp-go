// Command agent is a minimal ACP agent over stdio.
//
// It shows the pieces an agent implementation needs: the required Agent
// methods, session state through an embedded acp.SessionManager, streaming
// updates with acp.SessionStream, and a permission request before a
// destructive tool call.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	acp "github.com/ironpark/go-acp"
	schema "github.com/ironpark/go-acp/schema/v1"
)

// session holds the state the agent keeps per ACP session. Prompt and Cancel
// run on different goroutines, so the turn's cancel func is guarded.
type session struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

func (s *session) startTurn(cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel = cancel
}

func (s *session) cancelTurn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

// exampleAgent embeds a SessionManager, which supplies NewSession plus the
// optional session/load, session/list and session/delete handlers.
type exampleAgent struct {
	*acp.SessionManager[*session]
	client acp.Client
}

func (a *exampleAgent) Initialize(_ context.Context, _ *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	loadSession := true
	return &acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersion,
		AgentCapabilities: &schema.AgentCapabilities{
			LoadSession: &loadSession,
			SessionCapabilities: &schema.SessionCapabilities{
				List:   &schema.SessionListCapabilities{},
				Delete: &schema.SessionDeleteCapabilities{},
			},
		},
		AgentInfo: &schema.Implementation{Name: "example-agent", Version: "0.1.0"},
	}, nil
}

func (a *exampleAgent) Authenticate(_ context.Context, _ *acp.AuthenticateRequest) (*acp.AuthenticateResponse, error) {
	return &acp.AuthenticateResponse{}, nil
}

func (a *exampleAgent) Prompt(ctx context.Context, params *acp.PromptRequest) (*acp.PromptResponse, error) {
	current, ok := a.Session(params.SessionID)
	if !ok {
		return nil, acp.ErrResourceNotFound(fmt.Sprintf("session %s", params.SessionID))
	}

	// One turn at a time per session: replace the previous turn's cancel func.
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	current.startTurn(cancel)

	if err := a.runTurn(turnCtx, params.SessionID); err != nil {
		if turnCtx.Err() != nil {
			return &acp.PromptResponse{StopReason: schema.StopReasonCancelled}, nil
		}
		return nil, err
	}
	return &acp.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil
}

// Cancel ends the turn in progress. Prompt then returns the cancelled stop
// reason rather than an error.
func (a *exampleAgent) Cancel(_ context.Context, params *acp.CancelNotification) error {
	if current, ok := a.Session(params.SessionID); ok {
		current.cancelTurn()
	}
	return nil
}

func (a *exampleAgent) runTurn(ctx context.Context, sessionID acp.SessionID) error {
	stream := acp.NewSessionStream(a.client, sessionID)

	if err := stream.SendText(ctx, "Let me read the project first."); err != nil {
		return err
	}
	if err := pause(ctx); err != nil {
		return err
	}

	read := acp.ToolCallID("call_1")
	if err := stream.StartToolCall(ctx, read, "Reading project files", schema.ToolKindRead); err != nil {
		return err
	}
	if err := pause(ctx); err != nil {
		return err
	}
	if err := stream.CompleteToolCall(ctx, read, schema.NewToolCallContent(schema.ToolCallContentContent{
		Content: schema.NewContentBlock(schema.ContentBlockText{Text: "# My Project"}),
	})); err != nil {
		return err
	}

	edit := acp.ToolCallID("call_2")
	if err := stream.StartToolCall(ctx, edit, "Modifying configuration", schema.ToolKindEdit); err != nil {
		return err
	}

	// Editing a file is destructive, so ask the user first.
	pending := schema.ToolCallStatusPending
	kind := schema.ToolKindEdit
	title := "Modifying configuration"
	permission, err := a.client.RequestPermission(ctx, &acp.RequestPermissionRequest{
		SessionID: sessionID,
		ToolCall: schema.ToolCallUpdate{
			ToolCallID: edit,
			Title:      &title,
			Kind:       &kind,
			Status:     &pending,
			Locations:  []acp.ToolCallLocation{{Path: "/project/config.json"}},
		},
		Options: []schema.PermissionOption{
			{OptionID: "allow", Name: "Allow this change", Kind: schema.PermissionOptionKindAllowOnce},
			{OptionID: "reject", Name: "Skip this change", Kind: schema.PermissionOptionKindRejectOnce},
		},
	})
	if err != nil {
		return err
	}

	switch outcome := permission.Outcome.Variant().(type) {
	case schema.RequestPermissionOutcomeSelected:
		if outcome.OptionID != "allow" {
			if err := stream.FailToolCall(ctx, edit); err != nil {
				return err
			}
			return stream.SendText(ctx, " Skipping the configuration update.")
		}
		if err := stream.CompleteToolCall(ctx, edit); err != nil {
			return err
		}
		return stream.SendText(ctx, " Configuration updated.")
	default:
		// Cancelled, or an outcome added after this example was written.
		return stream.FailToolCall(ctx, edit)
	}
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

func main() {
	manager := acp.NewSessionManager(
		acp.NewMemoryStore[*session](),
		func(_ context.Context, _ *acp.NewSessionRequest) (acp.SessionID, *session, error) {
			return acp.GenerateSessionID(), &session{}, nil
		},
	)

	// Stdout carries the protocol, so logs go to stderr.
	logger := log.New(os.Stderr, "", log.LstdFlags)
	conn := acp.NewAgentSideConnection(func(c *acp.AgentSideConnection) acp.Agent {
		return &exampleAgent{SessionManager: manager, client: c}
	}, os.Stdin, os.Stdout,
		acp.WithErrorHandler(func(err error) { logger.Printf("acp: %v", err) }),
	)

	if err := conn.Start(context.Background()); err != nil {
		logger.Printf("connection error: %v", err)
		os.Exit(1)
	}
}
