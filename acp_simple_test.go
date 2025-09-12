package acp

import (
	"testing"
)

// Test basic ACP types and helper functions
func TestProtocolVersion(t *testing.T) {
	version := ProtocolVersion(CurrentProtocolVersion)
	if int(version) != CurrentProtocolVersion {
		t.Errorf("Expected protocol version %d, got %d", CurrentProtocolVersion, int(version))
	}
}

func TestSessionId(t *testing.T) {
	sessionId := SessionId("test-session-123")
	if string(sessionId) != "test-session-123" {
		t.Errorf("Expected session ID 'test-session-123', got '%s'", string(sessionId))
	}
}

func TestToolCallId(t *testing.T) {
	toolCallId := ToolCallId("tool-call-456")
	if string(toolCallId) != "tool-call-456" {
		t.Errorf("Expected tool call ID 'tool-call-456', got '%s'", string(toolCallId))
	}
}

func TestNewContentBlockText(t *testing.T) {
	text := "Hello, world!"
	contentBlock := NewContentBlockText(text)
	
	if !contentBlock.IsText() {
		t.Error("Expected content block to be text type")
	}
	
	textBlock := contentBlock.GetText()
	if textBlock.Text != text {
		t.Errorf("Expected text '%s', got '%s'", text, textBlock.Text)
	}
	
	if textBlock.Type != "text" {
		t.Errorf("Expected type 'text', got '%s'", textBlock.Type)
	}
}

func TestNewSessionUpdateAgentMessageChunk(t *testing.T) {
	text := "Agent response"
	contentBlock := NewContentBlockText(text)
	update := NewSessionUpdateAgentMessageChunk(contentBlock)
	
	if !update.IsAgentmessagechunk() {
		t.Error("Expected session update to be agent message chunk type")
	}
	
	chunk := update.GetAgentmessagechunk()
	if !chunk.Content.IsText() {
		t.Error("Expected chunk content to be text type")
	}
	
	if chunk.Content.GetText().Text != text {
		t.Errorf("Expected chunk text '%s', got '%s'", text, chunk.Content.GetText().Text)
	}
}

func TestNewRequestPermissionOutcomeSelected(t *testing.T) {
	optionId := PermissionOptionId("allow")
	outcome := NewRequestPermissionOutcomeSelected(optionId)
	
	if !outcome.IsSelected() {
		t.Error("Expected outcome to be selected type")
	}
	
	selected := outcome.GetSelected()
	if selected.OptionId != optionId {
		t.Errorf("Expected option ID '%s', got '%s'", optionId, selected.OptionId)
	}
}

func TestPermissionOptions(t *testing.T) {
	option := PermissionOption{
		Kind:     PermissionOptionKindAllowOnce,
		Name:     "Allow this operation",
		OptionId: PermissionOptionId("allow"),
	}
	
	if option.Kind != PermissionOptionKindAllowOnce {
		t.Errorf("Expected kind '%s', got '%s'", PermissionOptionKindAllowOnce, option.Kind)
	}
	
	if option.Name != "Allow this operation" {
		t.Errorf("Expected name 'Allow this operation', got '%s'", option.Name)
	}
	
	if option.OptionId != PermissionOptionId("allow") {
		t.Errorf("Expected option ID 'allow', got '%s'", option.OptionId)
	}
}

func TestToolCallLocation(t *testing.T) {
	location := ToolCallLocation{
		Path: "/path/to/file.txt",
		Line: nil, // Line is optional
	}
	
	if location.Path != "/path/to/file.txt" {
		t.Errorf("Expected path '/path/to/file.txt', got '%s'", location.Path)
	}
	
	if location.Line != nil {
		t.Errorf("Expected line to be nil, got %v", location.Line)
	}
}

func TestToolCallContentContent(t *testing.T) {
	text := "Tool output"
	contentBlock := NewContentBlockText(text)
	toolCallContent := NewToolCallContentContent(contentBlock)
	
	// Test that we can create and access the content
	if !toolCallContent.IsContent() {
		t.Error("Expected tool call content to be content type")
	}
	
	content := toolCallContent.GetContent()
	if !content.Content.IsText() {
		t.Error("Expected content to be text type")
	}
	
	if content.Content.GetText().Text != text {
		t.Errorf("Expected text '%s', got '%s'", text, content.Content.GetText().Text)
	}
}

func TestStopReasons(t *testing.T) {
	reasons := []StopReason{
		StopReasonEndTurn,
		StopReasonMaxTokens,
		StopReasonMaxTurnRequests,
		StopReasonRefusal,
		StopReasonCancelled,
	}
	
	expectedReasons := []string{
		"end_turn",
		"max_tokens",
		"max_turn_requests",
		"refusal",
		"cancelled",
	}
	
	for i, reason := range reasons {
		if string(reason) != expectedReasons[i] {
			t.Errorf("Expected reason '%s', got '%s'", expectedReasons[i], string(reason))
		}
	}
}

func TestToolKinds(t *testing.T) {
	kinds := []ToolKind{
		ToolKindRead,
		ToolKindEdit,
		ToolKindDelete,
		ToolKindMove,
		ToolKindSearch,
		ToolKindExecute,
		ToolKindThink,
		ToolKindFetch,
		ToolKindOther,
	}
	
	expectedKinds := []string{
		"read",
		"edit",
		"delete",
		"move",
		"search",
		"execute",
		"think",
		"fetch",
		"other",
	}
	
	for i, kind := range kinds {
		if string(kind) != expectedKinds[i] {
			t.Errorf("Expected kind '%s', got '%s'", expectedKinds[i], string(kind))
		}
	}
}

func TestToolCallStatuses(t *testing.T) {
	statuses := []ToolCallStatus{
		ToolCallStatusPending,
		ToolCallStatusInProgress,
		ToolCallStatusCompleted,
		ToolCallStatusFailed,
	}
	
	expectedStatuses := []string{
		"pending",
		"in_progress", 
		"completed",
		"failed",
	}
	
	for i, status := range statuses {
		if string(status) != expectedStatuses[i] {
			t.Errorf("Expected status '%s', got '%s'", expectedStatuses[i], string(status))
		}
	}
}

// Test helper functions
func TestHelperFunctions(t *testing.T) {
	// Test StringPtr
	str := "test string"
	strPtr := StringPtr(str)
	if strPtr == nil || *strPtr != str {
		t.Errorf("StringPtr failed: expected '%s', got %v", str, strPtr)
	}
	
	// Test ToolKindPtr
	kind := ToolKindRead
	kindPtr := ToolKindPtr(kind)
	if kindPtr == nil || *kindPtr != kind {
		t.Errorf("ToolKindPtr failed: expected '%s', got %v", kind, kindPtr)
	}
	
	// Test ToolCallStatusPtr
	status := ToolCallStatusCompleted
	statusPtr := ToolCallStatusPtr(status)
	if statusPtr == nil || *statusPtr != status {
		t.Errorf("ToolCallStatusPtr failed: expected '%s', got %v", status, statusPtr)
	}
}

// Test request/response structures
func TestInitializeRequest(t *testing.T) {
	req := &InitializeRequest{
		ProtocolVersion: ProtocolVersion(1),
		ClientCapabilities: &ClientCapabilities{
			Fs: &FileSystemCapability{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: false,
		},
	}
	
	if req.ProtocolVersion != ProtocolVersion(1) {
		t.Errorf("Expected protocol version 1, got %d", req.ProtocolVersion)
	}
	
	if req.ClientCapabilities == nil {
		t.Error("Expected client capabilities, got nil")
	} else {
		if req.ClientCapabilities.Fs == nil {
			t.Error("Expected filesystem capabilities, got nil")
		} else {
			if !req.ClientCapabilities.Fs.ReadTextFile {
				t.Error("Expected ReadTextFile to be true")
			}
			if !req.ClientCapabilities.Fs.WriteTextFile {
				t.Error("Expected WriteTextFile to be true") 
			}
		}
		if req.ClientCapabilities.Terminal {
			t.Error("Expected Terminal to be false")
		}
	}
}

func TestInitializeResponse(t *testing.T) {
	resp := &InitializeResponse{
		ProtocolVersion: ProtocolVersion(1),
		AgentCapabilities: &AgentCapabilities{
			LoadSession: true,
			McpCapabilities: &McpCapabilities{
				Http: false,
				Sse:  true,
			},
			PromptCapabilities: &PromptCapabilities{
				Audio:           false,
				EmbeddedContext: true,
				Image:           true,
			},
		},
		AuthMethods: []AuthMethod{
			{
				Id:          AuthMethodId("oauth"),
				Name:        "OAuth Authentication",
				Description: "OAuth 2.0 authentication",
			},
		},
	}
	
	if resp.ProtocolVersion != ProtocolVersion(1) {
		t.Errorf("Expected protocol version 1, got %d", resp.ProtocolVersion)
	}
	
	if resp.AgentCapabilities == nil {
		t.Error("Expected agent capabilities, got nil")
		return
	}
	
	if !resp.AgentCapabilities.LoadSession {
		t.Error("Expected LoadSession to be true")
	}
	
	if resp.AgentCapabilities.McpCapabilities == nil {
		t.Error("Expected MCP capabilities, got nil")
	} else {
		if resp.AgentCapabilities.McpCapabilities.Http {
			t.Error("Expected HTTP to be false")
		}
		if !resp.AgentCapabilities.McpCapabilities.Sse {
			t.Error("Expected SSE to be true")
		}
	}
	
	if resp.AgentCapabilities.PromptCapabilities == nil {
		t.Error("Expected prompt capabilities, got nil")
	} else {
		if resp.AgentCapabilities.PromptCapabilities.Audio {
			t.Error("Expected Audio to be false")
		}
		if !resp.AgentCapabilities.PromptCapabilities.EmbeddedContext {
			t.Error("Expected EmbeddedContext to be true")
		}
		if !resp.AgentCapabilities.PromptCapabilities.Image {
			t.Error("Expected Image to be true")
		}
	}
	
	if len(resp.AuthMethods) != 1 {
		t.Errorf("Expected 1 auth method, got %d", len(resp.AuthMethods))
	} else {
		authMethod := resp.AuthMethods[0]
		if authMethod.Id != AuthMethodId("oauth") {
			t.Errorf("Expected auth method ID 'oauth', got '%s'", authMethod.Id)
		}
		if authMethod.Name != "OAuth Authentication" {
			t.Errorf("Expected auth method name 'OAuth Authentication', got '%s'", authMethod.Name)
		}
		if authMethod.Description != "OAuth 2.0 authentication" {
			t.Errorf("Expected auth method description 'OAuth 2.0 authentication', got '%s'", authMethod.Description)
		}
	}
}