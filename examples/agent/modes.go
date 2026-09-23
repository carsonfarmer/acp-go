package main

import (
	"context"
	"fmt"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
)

// The agent offers two modes. Each changes what the agent does: in ask mode
// it requests permission before editing a file, in auto mode it edits
// without asking. Clients show the modes and let the user switch.
const (
	askMode  acp1.SessionModeID = "ask"
	autoMode acp1.SessionModeID = "auto"
)

var modes = []acp1.SessionMode{
	{ID: askMode, Name: "Ask", Description: new("Ask before changing files")},
	{ID: autoMode, Name: "Auto", Description: new("Change files without asking")},
}

// SessionModes tells the client which modes the session has and which one
// is active; the embedded manager reports it when a session is created or
// resumed.
func (s *session) SessionModes() *acp1.SessionModeState {
	return &acp1.SessionModeState{CurrentModeID: s.currentMode(), AvailableModes: modes}
}

// SetSessionMode switches the session's mode; implementing it makes
// session/set_mode available.
func (a *exampleAgent) SetSessionMode(ctx context.Context, params *acp1.SetSessionModeRequest) (*acp1.SetSessionModeResponse, error) {
	sess, err := a.Lookup(ctx, params.SessionID)
	if err != nil {
		return nil, err
	}
	if params.ModeID != askMode && params.ModeID != autoMode {
		return nil, acp.InvalidParams(fmt.Sprintf("unknown mode %q", params.ModeID))
	}
	sess.mu.Lock()
	sess.mode = params.ModeID
	sess.mu.Unlock()
	return &acp1.SetSessionModeResponse{}, nil
}

func (s *session) currentMode() acp1.SessionModeID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}
