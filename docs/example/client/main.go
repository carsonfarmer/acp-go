// Command client is an interactive ACP client for any agent over stdio.
//
//	go run ./docs/example/client                          # the sibling agent example
//	go run ./docs/example/client go run ./docs/example/echo # any agent command
//
// It shows the client side of ACP: spawning an agent, driving turns with
// ClientSession and Turn, rendering updates with a type switch over the
// SessionUpdate union, cancelling a turn on Ctrl-C, answering permission
// requests, serving the optional file system methods the agent may call, and
// calling a typed extension method with acp.CallExt.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	acp "github.com/ironpark/go-acp"
	"github.com/ironpark/go-acp/acpv1"
)

// exampleClient implements acpv1.Client, plus acpv1.FileReader and
// acpv1.FileWriter, which ClientCapabilitiesOf turns into the fs capabilities
// it advertises.
type exampleClient struct {
	// input is shared by the prompt loop and permission requests, so input
	// typed ahead is not lost in a discarded buffer.
	input *bufio.Reader
}

// SessionUpdate receives every update the agent sends. This client renders
// the updates of its own turns from Turn.Updates instead, so it has nothing
// to do here; a UI that shows background activity would update its state.
func (c *exampleClient) SessionUpdate(context.Context, *acpv1.SessionNotification) error {
	return nil
}

// render prints one update with a type switch over the SessionUpdate union.
func render(update acpv1.SessionUpdate) {
	switch update := update.Variant().(type) {
	case acpv1.SessionUpdateAgentMessageChunk:
		if text, ok := acpv1.TextOf(update.Content); ok {
			fmt.Print(text)
		} else {
			fmt.Print("[non-text content]")
		}
	case acpv1.SessionUpdateAgentThoughtChunk:
		if text, ok := acpv1.TextOf(update.Content); ok {
			fmt.Printf("\n💭 %s", text)
		}
	case acpv1.SessionUpdateToolCall:
		fmt.Printf("\n🔧 %s", update.Title)
		if update.Status != nil {
			fmt.Printf(" (%s)", *update.Status)
		}
		fmt.Println()
	case acpv1.SessionUpdateToolCallUpdate:
		fmt.Printf("🔧 %s", update.ToolCallID)
		if update.Status != nil {
			fmt.Printf(": %s", *update.Status)
		}
		fmt.Println()
	case acpv1.SessionUpdatePlan:
		fmt.Printf("\n📋 plan with %d entries\n", len(update.Entries))
	default:
		// Includes acpv1.SessionUpdateUnknown: updates newer than this SDK
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

	for {
		fmt.Print("\nChoose an option: ")
		answer, err := c.input.ReadString('\n')
		if err != nil {
			return nil, err
		}
		choice, err := strconv.Atoi(strings.TrimSpace(answer))
		if err != nil || choice < 1 || choice > len(params.Options) {
			fmt.Printf("Enter a number between 1 and %d.\n", len(params.Options))
			continue
		}
		return &acpv1.RequestPermissionResponse{
			Outcome: acpv1.NewRequestPermissionOutcome(acpv1.RequestPermissionOutcomeSelected{
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

// pingResult is the agent example's reply to its _example.com/ping extension.
type pingResult struct {
	Reply string `json:"reply"`
}

func main() {
	verbose := flag.Bool("v", false, "show the agent's stderr")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: client [-v] [agent command...]\n\nWithout a command, it builds and runs the sibling agent example.\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if err := run(context.Background(), flag.Args(), *verbose); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, command []string, verbose bool) error {
	if len(command) == 0 {
		binary, cleanup, err := buildAgent()
		if err != nil {
			return err
		}
		defer cleanup()
		command = []string{binary}
	}
	cmd := exec.Command(command[0], command[1:]...)
	if !verbose {
		cmd.Stderr = io.Discard // agent logs would interleave with the conversation
	}

	client := &exampleClient{input: bufio.NewReader(os.Stdin)}
	agent, err := acpv1.SpawnAgent(ctx, cmd, func(*acpv1.ClientSideConnection) acpv1.Client {
		return client
	})
	if err != nil {
		return fmt.Errorf("spawn agent: %w", err)
	}
	defer agent.Close()

	initialized, err := agent.Initialize(ctx, &acpv1.InitializeRequest{
		ProtocolVersion:    acpv1.ProtocolVersion,
		ClientCapabilities: acpv1.ClientCapabilitiesOf(client),
		ClientInfo:         &acpv1.Implementation{Name: "example-client", Version: "0.1.0"},
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
	fmt.Printf("Session %s. Type a message, /ping <text> to call the agent's\nextension method, Ctrl-C to cancel a turn, or Ctrl-D to quit.\n", session.ID)

	for {
		fmt.Print("\n> ")
		line, err := client.input.ReadString('\n')
		if errors.Is(err, io.EOF) {
			fmt.Println()
			return nil
		}
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		switch message, isPing := strings.CutPrefix(line, "/ping "); {
		case line == "":
		case isPing:
			result, err := acp.CallExt[pingResult](ctx, agent, "_example.com/ping", map[string]string{"message": message})
			if err != nil {
				fmt.Printf("ping: %v\n", err) // agents other than the example answer "method not found"
				continue
			}
			fmt.Println(result.Reply)
		default:
			if err := prompt(ctx, session, line); err != nil {
				return err
			}
		}
	}
}

// prompt runs one turn, rendering its updates as they arrive.
func prompt(ctx context.Context, session *acpv1.ClientSession, text string) error {
	turn, err := session.Prompt(ctx, acpv1.TextBlock(text))
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}

	// While the turn runs, Ctrl-C asks the agent to stop instead of ending
	// the client; the turn then ends with StopReasonCancelled.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	go func() {
		select {
		case <-interrupt:
			_ = session.Cancel(ctx)
		case <-turn.Done():
		}
	}()

	for update := range turn.Updates() {
		render(update)
	}
	result, err := turn.Wait()
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	fmt.Printf("\n[%s]\n", result.StopReason)
	return nil
}

// buildAgent compiles the sibling agent example into a temporary directory
// and returns its path.
func buildAgent() (binary string, cleanup func(), err error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", nil, fmt.Errorf("cannot locate this source file")
	}
	dir, err := os.MkdirTemp("", "acp-example-agent")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { os.RemoveAll(dir) }
	binary = filepath.Join(dir, "agent")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	fmt.Println("Building agent...")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = filepath.Join(filepath.Dir(filepath.Dir(currentFile)), "agent")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("build agent: %w", err)
	}
	return binary, cleanup, nil
}
