// Command client spawns the example agent and drives one prompt turn.
//
// It shows the client side of ACP: spawning an agent, driving a turn with
// ClientSession and Turn, rendering updates with a type switch over the
// SessionUpdate union, answering permission requests, and serving the
// optional file system methods the agent may call.
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
	"github.com/ironpark/go-acp/acpv1"
	schema "github.com/ironpark/go-acp/schema/v1"
)

// exampleClient implements acpv1.Client, plus acpv1.FileReader and
// acpv1.FileWriter, which ClientCapabilitiesOf turns into the fs capabilities
// it advertises.
type exampleClient struct{}

// SessionUpdate receives every update the agent sends. This client renders
// the updates of its own turns from Turn.Updates instead, so it has nothing
// to do here; a UI that shows background activity would update its state.
func (c *exampleClient) SessionUpdate(context.Context, *acpv1.SessionNotification) error {
	return nil
}

// render prints one update with a type switch over the SessionUpdate union.
func render(update acpv1.SessionUpdate) {
	switch update := update.Variant().(type) {
	case schema.SessionUpdateAgentMessageChunk:
		if text, ok := acpv1.TextOf(update.Content); ok {
			fmt.Print(text)
		} else {
			fmt.Print("[non-text content]")
		}
	case schema.SessionUpdateAgentThoughtChunk:
		if text, ok := acpv1.TextOf(update.Content); ok {
			fmt.Printf("\n💭 %s", text)
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
	default:
		// Includes schema.SessionUpdateUnknown: updates newer than this SDK
		// are safe to ignore.
	}
}

func (c *exampleClient) RequestPermission(_ context.Context, params *acpv1.RequestPermissionRequest) (*acpv1.RequestPermissionResponse, error) {
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
		return &acpv1.RequestPermissionResponse{
			Outcome: schema.NewRequestPermissionOutcome(schema.RequestPermissionOutcomeSelected{
				OptionID: params.Options[choice-1].OptionID,
			}),
		}, nil
	}
}

func (c *exampleClient) ReadTextFile(_ context.Context, params *acpv1.ReadTextFileRequest) (*acpv1.ReadTextFileResponse, error) {
	content, err := os.ReadFile(params.Path)
	if err != nil {
		return nil, acp.ErrResourceNotFound(params.Path)
	}
	return &acpv1.ReadTextFileResponse{Content: string(content)}, nil
}

func (c *exampleClient) WriteTextFile(_ context.Context, params *acpv1.WriteTextFileRequest) (*acpv1.WriteTextFileResponse, error) {
	if err := os.WriteFile(params.Path, []byte(params.Content), 0o644); err != nil {
		return nil, err
	}
	return &acpv1.WriteTextFileResponse{}, nil
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

	client := &exampleClient{}
	agent, err := acpv1.SpawnAgent(ctx, exec.Command(agentBinary), func(*acpv1.ClientSideConnection) acpv1.Client {
		return client
	})
	if err != nil {
		return fmt.Errorf("spawn agent: %w", err)
	}
	defer agent.Close()

	initialized, err := agent.Initialize(ctx, &acpv1.InitializeRequest{
		ProtocolVersion:    acpv1.ProtocolVersion,
		ClientCapabilities: acpv1.ClientCapabilitiesOf(client),
		ClientInfo:         &schema.Implementation{Name: "example-client", Version: "0.1.0"},
	})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	fmt.Printf("Connected to agent (protocol v%d)\n", initialized.ProtocolVersion)

	cwd, _ := os.Getwd()
	session, err := agent.StartSession(ctx, &acpv1.NewSessionRequest{Cwd: cwd})
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	fmt.Printf("Created session: %s\nUser: Hello, agent!\n\n", session.ID)

	turn, err := session.Prompt(ctx, acpv1.TextBlock("Hello, agent!"))
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	for update := range turn.Updates() {
		render(update)
	}
	result, err := turn.Wait()
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
