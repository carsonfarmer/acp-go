package main

import (
	"context"
	"fmt"
	"slices"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
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

// SessionModes tells the client which modes the session has and which one
// is active. The embedded manager reports it when a session is created or
// resumed, and LoadSession when one is loaded.
func (s *session) SessionModes() *acp1.SessionModeState {
	return &acp1.SessionModeState{CurrentModeID: s.currentMode(), AvailableModes: modes}
}

func (a *openAgent) SetSessionMode(ctx context.Context, params *acp1.SetSessionModeRequest) (*acp1.SetSessionModeResponse, error) {
	sess, err := a.Lookup(ctx, params.SessionID)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(modes, func(m acp1.SessionMode) bool { return m.ID == params.ModeID }) {
		return nil, acp.InvalidParams(fmt.Sprintf("unknown mode %q", params.ModeID))
	}
	sess.setMode(params.ModeID)
	a.save(ctx, params.SessionID, sess)
	return &acp1.SetSessionModeResponse{}, nil
}

func (s *session) currentMode() acp1.SessionModeID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

func (s *session) setMode(mode acp1.SessionModeID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = mode
}
