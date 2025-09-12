package acp

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestConnection represents a bidirectional pipe similar to TypeScript's TransformStream
type TestConnection struct {
	clientToAgent *io.PipeReader
	agentToClient *io.PipeReader
	clientWriter  *io.PipeWriter
	agentWriter   *io.PipeWriter
}

func NewTestConnection() *TestConnection {
	clientReader, agentWriter := io.Pipe()
	agentReader, clientWriter := io.Pipe()

	return &TestConnection{
		clientToAgent: clientReader,
		agentToClient: agentReader,
		clientWriter:  clientWriter,
		agentWriter:   agentWriter,
	}
}

func (tc *TestConnection) Close() {
	tc.clientWriter.Close()
	tc.agentWriter.Close()
	tc.clientToAgent.Close()
	tc.agentToClient.Close()
}

// TestClient implements Client interface for integration testing
type TestClient struct {
	writeTextFileHandler     func(*WriteTextFileRequest) error
	readTextFileHandler      func(*ReadTextFileRequest) (*ReadTextFileResponse, error)
	requestPermissionHandler func(*RequestPermissionRequest) (*RequestPermissionResponse, error)
	sessionUpdateHandler     func(*SessionNotification) error
}

func (c *TestClient) WriteTextFile(ctx context.Context, params *WriteTextFileRequest) error {
	if c.writeTextFileHandler != nil {
		return c.writeTextFileHandler(params)
	}
	return nil
}

func (c *TestClient) ReadTextFile(ctx context.Context, params *ReadTextFileRequest) (*ReadTextFileResponse, error) {
	if c.readTextFileHandler != nil {
		return c.readTextFileHandler(params)
	}
	return &ReadTextFileResponse{Content: "Mock file content"}, nil
}

func (c *TestClient) RequestPermission(ctx context.Context, params *RequestPermissionRequest) (*RequestPermissionResponse, error) {
	if c.requestPermissionHandler != nil {
		return c.requestPermissionHandler(params)
	}
	return &RequestPermissionResponse{
		Outcome: NewRequestPermissionOutcomeSelected(PermissionOptionId("allow")),
	}, nil
}

func (c *TestClient) SessionUpdate(ctx context.Context, params *SessionNotification) error {
	if c.sessionUpdateHandler != nil {
		return c.sessionUpdateHandler(params)
	}
	return nil
}

func (c *TestClient) CreateTerminal(ctx context.Context, params *CreateTerminalRequest) (*CreateTerminalResponse, error) {
	return &CreateTerminalResponse{TerminalId: "mock-terminal"}, nil
}

func (c *TestClient) TerminalOutput(ctx context.Context, params *TerminalOutputRequest) (*TerminalOutputResponse, error) {
	return &TerminalOutputResponse{Output: "mock output", Truncated: false}, nil
}

func (c *TestClient) ReleaseTerminal(ctx context.Context, params *ReleaseTerminalRequest) error {
	return nil
}

func (c *TestClient) WaitForTerminalExit(ctx context.Context, params *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error) {
	exitCode := int64(0)
	return &WaitForTerminalExitResponse{ExitCode: &exitCode}, nil
}

func (c *TestClient) KillTerminalCommand(ctx context.Context, params *KillTerminalCommandRequest) error {
	return nil
}

// TestAgent implements Agent interface for integration testing
type TestAgent struct {
	initializeHandler     func(*InitializeRequest) (*InitializeResponse, error)
	newSessionHandler     func(*NewSessionRequest) (*NewSessionResponse, error)
	loadSessionHandler    func(*LoadSessionRequest) (*LoadSessionResponse, error)
	setSessionModeHandler func(*SetSessionModeRequest) error
	authenticateHandler   func(*AuthenticateRequest) error
	promptHandler         func(*PromptRequest) (*PromptResponse, error)
	cancelHandler         func(*CancelNotification) error
}

func (a *TestAgent) Initialize(ctx context.Context, params *InitializeRequest) (*InitializeResponse, error) {
	if a.initializeHandler != nil {
		return a.initializeHandler(params)
	}
	return &InitializeResponse{
		ProtocolVersion:   ProtocolVersion(CurrentProtocolVersion),
		AgentCapabilities: &AgentCapabilities{LoadSession: false},
		AuthMethods:       []AuthMethod{},
	}, nil
}

func (a *TestAgent) NewSession(ctx context.Context, params *NewSessionRequest) (*NewSessionResponse, error) {
	if a.newSessionHandler != nil {
		return a.newSessionHandler(params)
	}
	return &NewSessionResponse{SessionId: SessionId("test-session")}, nil
}

func (a *TestAgent) LoadSession(ctx context.Context, params *LoadSessionRequest) (*LoadSessionResponse, error) {
	if a.loadSessionHandler != nil {
		return a.loadSessionHandler(params)
	}
	return &LoadSessionResponse{}, nil
}

func (a *TestAgent) SetSessionMode(ctx context.Context, params *SetSessionModeRequest) error {
	if a.setSessionModeHandler != nil {
		return a.setSessionModeHandler(params)
	}
	return nil
}

func (a *TestAgent) Authenticate(ctx context.Context, params *AuthenticateRequest) error {
	if a.authenticateHandler != nil {
		return a.authenticateHandler(params)
	}
	return nil
}

func (a *TestAgent) Prompt(ctx context.Context, params *PromptRequest) (*PromptResponse, error) {
	if a.promptHandler != nil {
		return a.promptHandler(params)
	}
	return &PromptResponse{StopReason: StopReasonEndTurn}, nil
}

func (a *TestAgent) Cancel(ctx context.Context, params *CancelNotification) error {
	if a.cancelHandler != nil {
		return a.cancelHandler(params)
	}
	return nil
}

func TestBidirectionalCommunicationErrors(t *testing.T) {
	conn := NewTestConnection()
	defer conn.Close()

	// Create client that throws errors
	testClient := &TestClient{
		writeTextFileHandler: func(*WriteTextFileRequest) error {
			return fmt.Errorf("write failed")
		},
		readTextFileHandler: func(*ReadTextFileRequest) (*ReadTextFileResponse, error) {
			return nil, fmt.Errorf("read failed")
		},
		requestPermissionHandler: func(*RequestPermissionRequest) (*RequestPermissionResponse, error) {
			return nil, fmt.Errorf("permission denied")
		},
	}

	// Create agent that throws errors
	testAgent := &TestAgent{
		initializeHandler: func(*InitializeRequest) (*InitializeResponse, error) {
			return nil, fmt.Errorf("failed to initialize")
		},
		newSessionHandler: func(req *NewSessionRequest) (*NewSessionResponse, error) {
			return nil, fmt.Errorf("failed to create session")
		},
		loadSessionHandler: func(*LoadSessionRequest) (*LoadSessionResponse, error) {
			return nil, fmt.Errorf("failed to load session")
		},
		authenticateHandler: func(*AuthenticateRequest) error {
			return fmt.Errorf("authentication failed")
		},
		promptHandler: func(*PromptRequest) (*PromptResponse, error) {
			return nil, fmt.Errorf("prompt failed")
		},
	}

	// Set up connections
	agentConnection := NewClientSideConnection(testClient, conn.agentWriter, conn.agentToClient)
	clientConnection := NewAgentSideConnection(testAgent, conn.clientToAgent, conn.clientWriter)

	ctx := context.Background()

	// Start the connections
	go func() {
		_ = agentConnection.Start(ctx)
	}()
	go func() {
		_ = clientConnection.Start(ctx)
	}()

	// Give connections time to start
	time.Sleep(100 * time.Millisecond)

	// Test error handling in client->agent direction (use clientConnection to call client methods that should error)
	// Use a retry mechanism to handle race conditions in bidirectional communication
	var writeErr error
	for i := 0; i < 3; i++ {
		writeErr = clientConnection.Client().WriteTextFile(ctx, &WriteTextFileRequest{
			Path:      "/test.txt",
			Content:   "test",
			SessionId: SessionId("test-session"),
		})
		if writeErr != nil {
			break // Got expected error
		}
		// Race condition occurred, retry after small delay
		time.Sleep(10 * time.Millisecond)
	}
	if writeErr == nil {
		t.Error("Expected error from WriteTextFile, got nil after 3 retries")
	} else if !strings.Contains(writeErr.Error(), "write failed") {
		t.Errorf("Expected error containing 'write failed', got: %v", writeErr)
	}

	// Test error handling in agent->client direction with retry mechanism
	var sessionErr error  
	for i := 0; i < 3; i++ {
		_, sessionErr = agentConnection.NewSession(ctx, &NewSessionRequest{
			Cwd:        "/test",
			McpServers: []McpServer{},
		})
		if sessionErr != nil {
			break // Got expected error
		}
		// Race condition occurred, retry after small delay
		time.Sleep(10 * time.Millisecond)
	}
	if sessionErr == nil {
		t.Error("Expected error from NewSession, got nil after 3 retries")
	} else if !strings.Contains(sessionErr.Error(), "failed to create session") {
		t.Errorf("Expected error containing 'failed to create session', got: %v", sessionErr)
	}
}

func TestConcurrentRequests(t *testing.T) {
	conn := NewTestConnection()
	defer conn.Close()

	var requestCount int64

	// Create client
	testClient := &TestClient{
		writeTextFileHandler: func(params *WriteTextFileRequest) error {
			atomic.AddInt64(&requestCount, 1)
			currentCount := atomic.LoadInt64(&requestCount)
			// Simulate work
			time.Sleep(40 * time.Millisecond)
			t.Logf("Write request %d completed", currentCount)
			return nil
		},
		readTextFileHandler: func(params *ReadTextFileRequest) (*ReadTextFileResponse, error) {
			return &ReadTextFileResponse{Content: fmt.Sprintf("Content of %s", params.Path)}, nil
		},
		requestPermissionHandler: func(*RequestPermissionRequest) (*RequestPermissionResponse, error) {
			return &RequestPermissionResponse{
				Outcome: NewRequestPermissionOutcomeSelected(PermissionOptionId("allow")),
			}, nil
		},
	}

	// Create agent
	testAgent := &TestAgent{
		initializeHandler: func(*InitializeRequest) (*InitializeResponse, error) {
			return &InitializeResponse{
				ProtocolVersion:   ProtocolVersion(1),
				AgentCapabilities: &AgentCapabilities{LoadSession: false},
				AuthMethods:       []AuthMethod{},
			}, nil
		},
		newSessionHandler: func(*NewSessionRequest) (*NewSessionResponse, error) {
			return &NewSessionResponse{SessionId: SessionId("test-session")}, nil
		},
		promptHandler: func(*PromptRequest) (*PromptResponse, error) {
			return &PromptResponse{StopReason: StopReasonEndTurn}, nil
		},
	}

	// Set up connections
	agentConnection := NewClientSideConnection(testClient, conn.agentWriter, conn.agentToClient)
	clientConnection := NewAgentSideConnection(testAgent, conn.clientToAgent, conn.clientWriter)

	ctx := context.Background()

	// Start the connections
	go func() {
		_ = agentConnection.Start(ctx)
	}()
	go func() {
		_ = clientConnection.Start(ctx)
	}()

	// Give connections time to start
	time.Sleep(100 * time.Millisecond)

	// Send multiple concurrent requests
	var wg sync.WaitGroup
	wg.Add(3)

	errors := make([]error, 3)

	for i := 0; i < 3; i++ {
		go func(index int) {
			defer wg.Done()
			err := clientConnection.Client().WriteTextFile(ctx, &WriteTextFileRequest{
				Path:      fmt.Sprintf("/file%d.txt", index+1),
				Content:   fmt.Sprintf("content%d", index+1),
				SessionId: SessionId("session1"),
			})
			errors[index] = err
		}(i)
	}

	wg.Wait()

	// Verify all requests completed successfully
	for i, err := range errors {
		if err != nil {
			t.Errorf("Request %d failed: %v", i, err)
		}
	}

	finalCount := atomic.LoadInt64(&requestCount)
	if finalCount != 3 {
		t.Errorf("Expected 3 requests, got %d", finalCount)
	}
}

func TestMessageOrdering(t *testing.T) {
	conn := NewTestConnection()
	defer conn.Close()

	messageLog := make([]string, 0)
	var logMutex sync.Mutex

	addToLog := func(msg string) {
		logMutex.Lock()
		defer logMutex.Unlock()
		messageLog = append(messageLog, msg)
	}

	// Create client
	testClient := &TestClient{
		writeTextFileHandler: func(params *WriteTextFileRequest) error {
			addToLog(fmt.Sprintf("writeTextFile called: %s", params.Path))
			return nil
		},
		readTextFileHandler: func(params *ReadTextFileRequest) (*ReadTextFileResponse, error) {
			addToLog(fmt.Sprintf("readTextFile called: %s", params.Path))
			return &ReadTextFileResponse{Content: "test content"}, nil
		},
		requestPermissionHandler: func(params *RequestPermissionRequest) (*RequestPermissionResponse, error) {
			addToLog(fmt.Sprintf("requestPermission called: %s", params.ToolCall.Title))
			return &RequestPermissionResponse{
				Outcome: NewRequestPermissionOutcomeSelected(PermissionOptionId("allow")),
			}, nil
		},
		sessionUpdateHandler: func(params *SessionNotification) error {
			addToLog("sessionUpdate called")
			return nil
		},
	}

	// Create agent
	testAgent := &TestAgent{
		initializeHandler: func(*InitializeRequest) (*InitializeResponse, error) {
			return &InitializeResponse{
				ProtocolVersion:   ProtocolVersion(1),
				AgentCapabilities: &AgentCapabilities{LoadSession: false},
				AuthMethods:       []AuthMethod{},
			}, nil
		},
		newSessionHandler: func(request *NewSessionRequest) (*NewSessionResponse, error) {
			addToLog(fmt.Sprintf("newSession called: %s", request.Cwd))
			return &NewSessionResponse{SessionId: SessionId("test-session")}, nil
		},
		loadSessionHandler: func(params *LoadSessionRequest) (*LoadSessionResponse, error) {
			addToLog(fmt.Sprintf("loadSession called: %s", params.SessionId))
			return &LoadSessionResponse{}, nil
		},
		authenticateHandler: func(params *AuthenticateRequest) error {
			addToLog(fmt.Sprintf("authenticate called: %s", params.MethodId))
			return nil
		},
		promptHandler: func(params *PromptRequest) (*PromptResponse, error) {
			addToLog(fmt.Sprintf("prompt called: %s", params.SessionId))
			return &PromptResponse{StopReason: StopReasonEndTurn}, nil
		},
		cancelHandler: func(params *CancelNotification) error {
			addToLog(fmt.Sprintf("cancelled called: %s", params.SessionId))
			return nil
		},
	}

	// Set up connections
	agentConnection := NewClientSideConnection(testClient, conn.agentWriter, conn.agentToClient)
	clientConnection := NewAgentSideConnection(testAgent, conn.clientToAgent, conn.clientWriter)

	ctx := context.Background()

	// Start the connections
	go func() {
		_ = agentConnection.Start(ctx)
	}()
	go func() {
		_ = clientConnection.Start(ctx)
	}()

	// Give connections time to start
	time.Sleep(100 * time.Millisecond)

	// Send requests in specific order
	_, err := agentConnection.NewSession(ctx, &NewSessionRequest{
		Cwd:        "/test",
		McpServers: []McpServer{},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	err = clientConnection.Client().WriteTextFile(ctx, &WriteTextFileRequest{
		Path:      "/test.txt",
		Content:   "test",
		SessionId: SessionId("test-session"),
	})
	if err != nil {
		t.Fatalf("WriteTextFile failed: %v", err)
	}

	_, err = clientConnection.Client().ReadTextFile(ctx, &ReadTextFileRequest{
		Path:      "/test.txt",
		SessionId: SessionId("test-session"),
	})
	if err != nil {
		t.Fatalf("ReadTextFile failed: %v", err)
	}

	_, err = clientConnection.Client().RequestPermission(ctx, &RequestPermissionRequest{
		SessionId: SessionId("test-session"),
		ToolCall: ToolCallUpdate{
			ToolCallId: ToolCallId("tool-123"),
			Title:      "Execute command",
			Kind:       ToolKindPtr(ToolKindExecute),
			Status:     ToolCallStatusPtr(ToolCallStatusPending),
			Locations: []ToolCallLocation{
				{Path: "/usr/bin/ls"},
			},
		},
		Options: []PermissionOption{
			{
				Kind:     PermissionOptionKindAllowOnce,
				Name:     "Allow",
				OptionId: PermissionOptionId("allow"),
			},
			{
				Kind:     PermissionOptionKindRejectOnce,
				Name:     "Reject",
				OptionId: PermissionOptionId("reject"),
			},
		},
	})
	if err != nil {
		t.Fatalf("RequestPermission failed: %v", err)
	}

	// Give time for all operations to complete
	time.Sleep(200 * time.Millisecond)

	// Verify order
	expected := []string{
		"newSession called: /test",
		"writeTextFile called: /test.txt",
		"readTextFile called: /test.txt",
		"requestPermission called: Execute command",
	}

	logMutex.Lock()
	defer logMutex.Unlock()

	if len(messageLog) != len(expected) {
		t.Fatalf("Expected %d messages, got %d: %v", len(expected), len(messageLog), messageLog)
	}

	for i, expectedMsg := range expected {
		if i >= len(messageLog) || messageLog[i] != expectedMsg {
			t.Errorf("Message %d: expected '%s', got '%s'", i, expectedMsg, messageLog[i])
		}
	}
}

func TestNotifications(t *testing.T) {
	conn := NewTestConnection()
	defer conn.Close()

	notificationLog := make([]string, 0)
	var logMutex sync.Mutex

	addToLog := func(msg string) {
		logMutex.Lock()
		defer logMutex.Unlock()
		notificationLog = append(notificationLog, msg)
	}

	// Create client
	testClient := &TestClient{
		sessionUpdateHandler: func(notification *SessionNotification) error {
			if notification.Update.IsAgentmessagechunk() {
				chunk := notification.Update.GetAgentmessagechunk()
				if chunk.Content.IsText() {
					text := chunk.Content.GetText().Text
					addToLog(fmt.Sprintf("agent message: %s", text))
				}
			}
			return nil
		},
	}

	// Create agent
	testAgent := &TestAgent{
		cancelHandler: func(params *CancelNotification) error {
			addToLog(fmt.Sprintf("cancelled: %s", params.SessionId))
			return nil
		},
	}

	// Set up connections
	agentConnection := NewClientSideConnection(testClient, conn.agentWriter, conn.agentToClient)
	clientConnection := NewAgentSideConnection(testAgent, conn.clientToAgent, conn.clientWriter)

	ctx := context.Background()

	// Start the connections
	go func() {
		_ = agentConnection.Start(ctx)
	}()
	go func() {
		_ = clientConnection.Start(ctx)
	}()

	// Give connections time to start
	time.Sleep(100 * time.Millisecond)

	// Send notifications 
	// SessionUpdate: agent -> client (use clientConnection to simulate agent sending to client)
	err := clientConnection.Client().SessionUpdate(ctx, &SessionNotification{
		SessionId: SessionId("test-session"),
		Update: NewSessionUpdateAgentMessageChunk(
			NewContentBlockText("Hello from agent"),
		),
	})
	if err != nil {
		t.Fatalf("SessionUpdate failed: %v", err)
	}

	// Cancel: client -> agent (use agentConnection to send cancel notification)
	err = agentConnection.Cancel(ctx, &CancelNotification{
		SessionId: SessionId("test-session"),
	})
	if err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	// Wait for async handlers
	time.Sleep(200 * time.Millisecond)

	// Verify notifications were received
	logMutex.Lock()
	defer logMutex.Unlock()

	found1 := false
	found2 := false
	for _, msg := range notificationLog {
		if msg == "agent message: Hello from agent" {
			found1 = true
		}
		if msg == "cancelled: test-session" {
			found2 = true
		}
	}

	if !found1 {
		t.Errorf("Expected 'agent message: Hello from agent' in notifications: %v", notificationLog)
	}
	if !found2 {
		t.Errorf("Expected 'cancelled: test-session' in notifications: %v", notificationLog)
	}
}

func TestInitializeMethod(t *testing.T) {
	conn := NewTestConnection()
	defer conn.Close()

	// Create agent
	testAgent := &TestAgent{
		initializeHandler: func(params *InitializeRequest) (*InitializeResponse, error) {
			return &InitializeResponse{
				ProtocolVersion: params.ProtocolVersion,
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
						Name:        "OAuth",
						Description: "Authenticate with OAuth",
					},
				},
			}, nil
		},
	}

	// Set up connections
	agentConnection := NewClientSideConnection(&TestClient{}, conn.agentWriter, conn.agentToClient)
	clientConnection := NewAgentSideConnection(testAgent, conn.clientToAgent, conn.clientWriter)

	ctx := context.Background()

	// Start the connections
	go func() {
		_ = agentConnection.Start(ctx)
	}()
	go func() {
		_ = clientConnection.Start(ctx)
	}()

	// Give connections time to start
	time.Sleep(100 * time.Millisecond)

	// Test initialize request
	response, err := agentConnection.Initialize(ctx, &InitializeRequest{
		ProtocolVersion: ProtocolVersion(CurrentProtocolVersion),
		ClientCapabilities: &ClientCapabilities{
			Fs: &FileSystemCapability{
				ReadTextFile:  false,
				WriteTextFile: false,
			},
		},
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if response.ProtocolVersion != ProtocolVersion(CurrentProtocolVersion) {
		t.Errorf("Expected protocol version %d, got %d", CurrentProtocolVersion, response.ProtocolVersion)
	}

	if response.AgentCapabilities == nil {
		t.Fatal("Expected agent capabilities, got nil")
	}

	if !response.AgentCapabilities.LoadSession {
		t.Error("Expected LoadSession to be true")
	}

	if len(response.AuthMethods) != 1 {
		t.Errorf("Expected 1 auth method, got %d", len(response.AuthMethods))
	} else {
		authMethod := response.AuthMethods[0]
		if authMethod.Id != AuthMethodId("oauth") {
			t.Errorf("Expected auth method ID 'oauth', got '%s'", authMethod.Id)
		}
		if authMethod.Name != "OAuth" {
			t.Errorf("Expected auth method name 'OAuth', got '%s'", authMethod.Name)
		}
	}
}