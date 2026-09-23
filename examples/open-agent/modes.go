package main

import (
	"context"
	"fmt"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acp1"
)

// In ask mode the agent asks before the model writes a file or runs a
// command; in auto mode it lets the model do both without asking. Reading a
// file never asks.
const (
	askMode  acp1.SessionModeID = "ask"
	autoMode acp1.SessionModeID = "auto"
)

var modes = []acp1.SessionMode{
	{ID: askMode, Name: "Ask", Description: new("Ask before changing files or running commands")},
	{ID: autoMode, Name: "Auto", Description: new("Change files and run commands without asking")},
}

func modeState(current acp1.SessionModeID) *acp1.SessionModeState {
	return &acp1.SessionModeState{CurrentModeID: current, AvailableModes: modes}
}

// NewSession creates the session with the embedded manager, then tells the
// client which modes it has and which one is active.
func (a *openAgent) NewSession(ctx context.Context, params *acp1.NewSessionRequest) (*acp1.NewSessionResponse, error) {
	response, err := a.SessionManager.NewSession(ctx, params)
	if err != nil {
		return nil, err
	}
	response.Modes = modeState(askMode)
	return response, nil
}

func (a *openAgent) SetSessionMode(_ context.Context, params *acp1.SetSessionModeRequest) (*acp1.SetSessionModeResponse, error) {
	sess, err := a.Lookup(params.SessionID)
	if err != nil {
		return nil, err
	}
	if params.ModeID != askMode && params.ModeID != autoMode {
		return nil, acp.ErrInvalidParams(fmt.Sprintf("unknown mode %q", params.ModeID))
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
