package acp

import (
	"context"
	"testing"
)

func TestTerminalHandle(t *testing.T) {
	// Test creating a TerminalHandle
	sessionID := SessionId("test-session")
	terminalID := "test-terminal"
	
	// Create a mock connection (we don't need real connection for structure tests)
	conn := &Connection{}
	
	handle := NewTerminalHandle(terminalID, sessionID, conn)
	
	// Test that the handle was created correctly
	if handle.ID != terminalID {
		t.Errorf("Expected terminal ID %s, got %s", terminalID, handle.ID)
	}
	
	if handle.sessionID != sessionID {
		t.Errorf("Expected session ID %s, got %s", sessionID, handle.sessionID)
	}
	
	if handle.conn != conn {
		t.Error("Expected connection to be set")
	}
}

func TestTerminalHandleMatchesTypeScriptAPI(t *testing.T) {
	// This test documents that our Go TerminalHandle matches the TypeScript API structure
	// by verifying method signatures exist and compile
	
	conn := &Connection{}
	handle := NewTerminalHandle("test", SessionId("session"), conn)
	
	// Verify all methods exist by checking their signatures
	// These should compile successfully, proving the methods exist
	
	_ = func(ctx context.Context) (*TerminalOutputResponse, error) {
		return handle.CurrentOutput(ctx)  // matches TypeScript currentOutput()
	}
	
	_ = func(ctx context.Context) (*WaitForTerminalExitResponse, error) {
		return handle.WaitForExit(ctx)    // matches TypeScript waitForExit()
	}
	
	_ = func(ctx context.Context) error {
		return handle.Kill(ctx)           // matches TypeScript kill()
	}
	
	_ = func(ctx context.Context) error {
		return handle.Release(ctx)        // matches TypeScript release()
	}
	
	t.Log("All TypeScript-equivalent methods are implemented with correct signatures")
}