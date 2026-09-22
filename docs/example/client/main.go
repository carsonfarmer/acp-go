// Command client spawns the example agent and drives one prompt turn.
//
// It shows the client side of ACP: handling session updates with a type
// switch over the SessionUpdate union, answering permission requests, and
// serving the optional file system methods the agent may call.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	acp "github.com/ironpark/go-acp"
	schema "github.com/ironpark/go-acp/schema/v1"
)

// exampleClient implements acp.Client, plus acp.FileReader and acp.FileWriter
// for the capabilities it advertises during initialization.
type exampleClient struct{}

func (c *exampleClient) SessionUpdate(_ context.Context, params *acp.SessionNotification) error {
	switch update := params.Update.Variant().(type) {
	case schema.SessionUpdateAgentMessageChunk:
		if text, ok := update.Content.Variant().(schema.ContentBlockText); ok {
			fmt.Print(text.Text)
		} else {
			fmt.Print("[non-text content]")
		}
	case schema.SessionUpdateAgentThoughtChunk:
		if text, ok := update.Content.Variant().(schema.ContentBlockText); ok {
			fmt.Printf("\n💭 %s", text.Text)
		}
	case schema.SessionUpdateToolCall:
		fmt.Printf("\n🔧 %s", update.Title)
		if update.Status != nil {
			fmt.Printf(" (%s)", *update.Status)
		}
		fmt.Println()
	case schema.SessionUpdateToolCallUpdate:
		fmt.Printf("🔧 %s", update.ToolCallID)
		if update.Status != nil {
			fmt.Printf(": %s", *update.Status)
		}
		fmt.Println()
	case schema.SessionUpdatePlan:
		fmt.Printf("\n📋 plan with %d entries\n", len(update.Entries))
	}
	return nil
}

func (c *exampleClient) RequestPermission(_ context.Context, params *acp.RequestPermissionRequest) (*acp.RequestPermissionResponse, error) {
	title := ""
	if params.ToolCall.Title != nil {
		title = *params.ToolCall.Title
	}
	fmt.Printf("\n🔐 Permission requested: %s\n", title)
	for i, option := range params.Options {
		fmt.Printf("   %d. %s (%s)\n", i+1, option.Name, option.Kind)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\nChoose an option: ")
		answer, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		choice, err := strconv.Atoi(strings.TrimSpace(answer))
		if err != nil || choice < 1 || choice > len(params.Options) {
			fmt.Printf("Enter a number between 1 and %d.\n", len(params.Options))
			continue
		}
		return &acp.RequestPermissionResponse{
			Outcome: schema.NewRequestPermissionOutcome(schema.RequestPermissionOutcomeSelected{
				OptionID: params.Options[choice-1].OptionID,
			}),
		}, nil
	}
}

func (c *exampleClient) ReadTextFile(_ context.Context, params *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {
	content, err := os.ReadFile(params.Path)
	if err != nil {
		return nil, acp.ErrResourceNotFound(params.Path)
	}
	return &acp.ReadTextFileResponse{Content: string(content)}, nil
}

func (c *exampleClient) WriteTextFile(_ context.Context, params *acp.WriteTextFileRequest) (*acp.WriteTextFileResponse, error) {
	if err := os.WriteFile(params.Path, []byte(params.Content), 0o644); err != nil {
		return nil, err
	}
	return &acp.WriteTextFileResponse{}, nil
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	agentBinary, err := buildAgent()
	if err != nil {
		return err
	}

	conn, err := acp.SpawnAgent(ctx, func(*acp.ClientSideConnection) acp.Client {
		return &exampleClient{}
	}, agentBinary)
	if err != nil {
		return fmt.Errorf("spawn agent: %w", err)
	}
	defer conn.Close()

	go func() {
		if err := conn.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "connection error: %v\n", err)
		}
	}()

	enabled := true
	initialized, err := conn.Initialize(ctx, &acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersion,
		ClientCapabilities: &schema.ClientCapabilities{
			Fs: &schema.FileSystemCapabilities{ReadTextFile: &enabled, WriteTextFile: &enabled},
		},
		ClientInfo: &schema.Implementation{Name: "example-client", Version: "0.1.0"},
	})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	fmt.Printf("Connected to agent (protocol v%d)\n", initialized.ProtocolVersion)

	cwd, _ := os.Getwd()
	created, err := conn.NewSession(ctx, &acp.NewSessionRequest{Cwd: cwd, MCPServers: []schema.MCPServer{}})
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	fmt.Printf("Created session: %s\nUser: Hello, agent!\n\n", created.SessionID)

	result, err := conn.Prompt(ctx, &acp.PromptRequest{
		SessionID: created.SessionID,
		Prompt: []acp.ContentBlock{
			schema.NewContentBlock(schema.ContentBlockText{Text: "Hello, agent!"}),
		},
	})
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	fmt.Printf("\n\nAgent stopped: %s\n", result.StopReason)
	return nil
}

// buildAgent compiles the sibling agent example and returns its path.
func buildAgent() (string, error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate this source file")
	}
	agentDir := filepath.Join(filepath.Dir(filepath.Dir(currentFile)), "agent")
	binary := filepath.Join(agentDir, "agent")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	fmt.Println("Building agent...")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = agentDir
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return "", fmt.Errorf("build agent: %w", err)
	}
	return binary, nil
}
