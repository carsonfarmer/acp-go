package main

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/ironpark/go-acp/acp1"
)

// maxModelCalls bounds the model calls in one turn, so a model that keeps
// calling tools ends the turn with max_turn_requests.
const maxModelCalls = 20

// outputLimit caps the command output the client keeps and the file content
// sent to the model, in bytes.
const outputLimit = 64 << 10

var (
	readFileTool = tool{Type: "function", Function: toolFunction{
		Name:        "read_file",
		Description: "Read a text file. Optionally start at a 1-based line and read at most limit lines.",
		Parameters: jsontext.Value(`{"type":"object","properties":{
			"path":{"type":"string","description":"Absolute path, or relative to the project directory"},
			"line":{"type":"integer","minimum":1},
			"limit":{"type":"integer","minimum":1}},
			"required":["path"]}`),
	}}
	writeFileTool = tool{Type: "function", Function: toolFunction{
		Name:        "write_file",
		Description: "Create or overwrite a text file with the given content.",
		Parameters: jsontext.Value(`{"type":"object","properties":{
			"path":{"type":"string","description":"Absolute path, or relative to the project directory"},
			"content":{"type":"string","description":"The complete new content of the file"}},
			"required":["path","content"]}`),
	}}
	runCommandTool = tool{Type: "function", Function: toolFunction{
		Name:        "run_command",
		Description: "Run a shell command in the project directory and return its output and exit code.",
		Parameters: jsontext.Value(`{"type":"object","properties":{
			"command":{"type":"string"}},
			"required":["command"]}`),
	}}
)

// tools returns the tools the client can run for the model.
func (a *openAgent) tools() []tool {
	var tools []tool
	if a.readFiles {
		tools = append(tools, readFileTool)
	}
	if a.writeFiles {
		tools = append(tools, writeFileTool)
	}
	if a.terminal {
		tools = append(tools, runCommandTool)
	}
	return tools
}

func (a *openAgent) systemPrompt(sess *session, tools []tool) message {
	var b strings.Builder
	fmt.Fprintf(&b, "You are open-agent, a coding assistant working in the project at %s on %s.\n", sess.cwd, runtime.GOOS)
	if len(tools) == 0 {
		b.WriteString("You have no tools in this session, so answer from the conversation alone.\n")
	} else {
		b.WriteString("Use your tools to inspect and change the project instead of guessing. The user may reject a change or a command; if so, do not retry it.\n")
	}
	b.WriteString("Be concise.")
	return message{Role: "system", Content: b.String()}
}

// runTurn answers the prompt: it calls the model, runs the tools it asks
// for, and calls it again with their results until it answers without
// calling a tool.
//
// The history only ever holds whole exchanges: the prompt, then each
// assistant message together with the results of all its tool calls. A turn
// cancelled halfway so never leaves a tool call without a result, which the
// next request would be rejected for.
func (a *openAgent) runTurn(ctx context.Context, sessionID acp1.SessionID, sess *session, prompt string) (acp1.StopReason, error) {
	stream := acp1.NewSessionStream(a.client, sessionID)
	tools := a.tools()
	sess.commit(message{Role: "user", Content: prompt})

	for range maxModelCalls {
		messages := append([]message{a.systemPrompt(sess, tools)}, sess.messages()...)
		reply, err := a.llm.stream(ctx, messages, tools,
			func(text string) error { return stream.SendText(ctx, text) },
			func(text string) error { return stream.SendThought(ctx, text) },
		)
		if err != nil {
			return "", err
		}
		if err := a.reportUsage(ctx, stream, sess, reply.usage); err != nil {
			return "", err
		}

		if len(reply.message.ToolCalls) == 0 {
			sess.commit(reply.message)
			switch reply.finishReason {
			case "length":
				return acp1.StopReasonMaxTokens, nil
			case "content_filter":
				return acp1.StopReasonRefusal, nil
			}
			return acp1.StopReasonEndTurn, nil
		}

		exchange := []message{reply.message}
		for _, call := range reply.message.ToolCalls {
			result, err := a.runTool(ctx, stream, sess, tools, call)
			if err != nil {
				return "", err
			}
			exchange = append(exchange, message{Role: "tool", ToolCallID: call.ID, Content: result})
		}
		sess.commit(exchange...)
	}
	return acp1.StopReasonMaxTurnRequests, nil
}

// reportUsage tells the client how full the context window is and what the
// session has cost so far, when the model's context length is known.
func (a *openAgent) reportUsage(ctx context.Context, stream *acp1.SessionStream, sess *session, u *usage) error {
	if u == nil || a.llm.contextLength == 0 {
		return nil
	}
	cost := sess.addCost(u.Cost)
	return stream.SendUsage(ctx, float64(u.PromptTokens+u.CompletionTokens), float64(a.llm.contextLength),
		&acp1.Cost{Amount: cost, Currency: "USD"})
}

// action is a tool call ready to show the client and run.
type action struct {
	title     string
	kind      acp1.ToolKind
	locations []acp1.ToolCallLocation
	// run does the work and returns the result for the model and the
	// content to show the client. With an error, the tool call fails, and
	// the model gets the result if there is one, the error otherwise.
	run func(ctx context.Context, stream *acp1.SessionStream, id acp1.ToolCallID) (string, []acp1.ToolCallContent, error)
}

// runTool runs one tool call from the model as an ACP tool call and returns
// the result for the model. A failing tool is reported to the model, which
// can react to it; only a failure of the connection, or a cancelled turn,
// ends the turn.
//
// The ACP tool call id is generated rather than taken from the model: it must
// be unique in the session, and the model's ids are only unique per message.
func (a *openAgent) runTool(ctx context.Context, stream *acp1.SessionStream, sess *session, tools []tool, call toolCall) (string, error) {
	id := acp1.GenerateToolCallID()
	act, err := a.parseTool(sess, tools, call)
	if err != nil {
		if err := stream.StartToolCall(ctx, id, call.Function.Name, acp1.ToolKindOther); err != nil {
			return "", err
		}
		return "Error: " + err.Error(), stream.FailToolCall(ctx, id, acp1.ToolText(err.Error()))
	}

	if err := stream.StartToolCall(ctx, id, act.title, act.kind, act.locations...); err != nil {
		return "", err
	}
	result, content, err := act.run(ctx, stream, id)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		if content == nil {
			content = []acp1.ToolCallContent{acp1.ToolText(err.Error())}
		}
		if result == "" {
			result = "Error: " + err.Error()
		}
		return result, stream.FailToolCall(ctx, id, content...)
	}
	return result, stream.CompleteToolCall(ctx, id, content...)
}

// parseTool decodes the model's arguments into an action. A model can call a
// tool it was not offered, which the client may not support, so that is an
// error too.
func (a *openAgent) parseTool(sess *session, tools []tool, call toolCall) (*action, error) {
	if !slices.ContainsFunc(tools, func(t tool) bool { return t.Function.Name == call.Function.Name }) {
		return nil, fmt.Errorf("unknown tool %q", call.Function.Name)
	}
	args := []byte(call.Function.Arguments)
	switch call.Function.Name {
	case readFileTool.Function.Name:
		var params struct {
			Path  string  `json:"path"`
			Line  *uint32 `json:"line"`
			Limit *uint32 `json:"limit"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		path := sess.resolve(params.Path)
		return &action{
			title:     "Read " + params.Path,
			kind:      acp1.ToolKindRead,
			locations: []acp1.ToolCallLocation{{Path: path, Line: params.Line}},
			run: func(ctx context.Context, stream *acp1.SessionStream, _ acp1.ToolCallID) (string, []acp1.ToolCallContent, error) {
				return a.readFile(ctx, stream, path, params.Line, params.Limit)
			},
		}, nil

	case writeFileTool.Function.Name:
		var params struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		path := sess.resolve(params.Path)
		return &action{
			title:     "Write " + params.Path,
			kind:      acp1.ToolKindEdit,
			locations: []acp1.ToolCallLocation{{Path: path}},
			run: func(ctx context.Context, stream *acp1.SessionStream, id acp1.ToolCallID) (string, []acp1.ToolCallContent, error) {
				return a.writeFile(ctx, stream, sess, id, path, params.Content)
			},
		}, nil

	case runCommandTool.Function.Name:
		var params struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		return &action{
			title: params.Command,
			kind:  acp1.ToolKindExecute,
			run: func(ctx context.Context, stream *acp1.SessionStream, id acp1.ToolCallID) (string, []acp1.ToolCallContent, error) {
				return a.runCommand(ctx, stream, sess, id, params.Command)
			},
		}, nil
	}
	panic("unreachable: every offered tool is handled")
}

func (a *openAgent) readFile(ctx context.Context, stream *acp1.SessionStream, path string, line, limit *uint32) (string, []acp1.ToolCallContent, error) {
	file, err := a.client.ReadTextFile(ctx, &acp1.ReadTextFileRequest{
		SessionID: stream.SessionID(),
		Path:      path,
		Line:      line,
		Limit:     limit,
	})
	if err != nil {
		return "", nil, err
	}
	// The client shows that the file was read; the model gets its content.
	content := file.Content
	if len(content) > outputLimit {
		content = content[:outputLimit] + "\n[truncated; read the rest with line and limit]"
	}
	summary := fmt.Sprintf("Read %d lines", strings.Count(file.Content, "\n")+1)
	return content, []acp1.ToolCallContent{acp1.ToolText(summary)}, nil
}

// writeFile shows the change as a diff and, in ask mode, writes it only if
// the user allows it.
func (a *openAgent) writeFile(ctx context.Context, stream *acp1.SessionStream, sess *session, id acp1.ToolCallID, path, content string) (string, []acp1.ToolCallContent, error) {
	// A file the client cannot read is shown as a new one.
	var oldText *string
	if old, err := a.client.ReadTextFile(ctx, &acp1.ReadTextFileRequest{SessionID: stream.SessionID(), Path: path}); err == nil {
		oldText = &old.Content
	}
	diff := []acp1.ToolCallContent{acp1.ToolDiff(path, oldText, content)}

	if err := a.allow(ctx, stream, sess, id, "Write "+path, acp1.ToolKindEdit, diff, acp1.ToolCallLocation{Path: path}); err != nil {
		return "", diff, err
	}
	if _, err := a.client.WriteTextFile(ctx, &acp1.WriteTextFileRequest{
		SessionID: stream.SessionID(),
		Path:      path,
		Content:   content,
	}); err != nil {
		return "", diff, err
	}
	return "Wrote " + path, diff, nil
}

// runCommand runs a shell command in a terminal the client owns and embeds
// the terminal in the tool call, so the client shows the output as it runs.
func (a *openAgent) runCommand(ctx context.Context, stream *acp1.SessionStream, sess *session, id acp1.ToolCallID, command string) (string, []acp1.ToolCallContent, error) {
	if err := a.allow(ctx, stream, sess, id, command, acp1.ToolKindExecute, []acp1.ToolCallContent{acp1.ToolText(command)}); err != nil {
		return "", nil, err
	}

	shell, flag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shell, flag = "cmd", "/C"
	}
	terminal, err := a.client.NewTerminal(ctx, &acp1.CreateTerminalRequest{
		SessionID:       stream.SessionID(),
		Command:         shell,
		Args:            []string{flag, command},
		Cwd:             &sess.cwd,
		OutputByteLimit: new(float64(outputLimit)),
	})
	if err != nil {
		return "", nil, err
	}
	// Release frees the terminal; the client still shows its output in the
	// tool call.
	defer terminal.Release(context.WithoutCancel(ctx))
	content := []acp1.ToolCallContent{acp1.ToolTerminal(terminal.ID)}
	if err := stream.Send(ctx, acp1.SessionUpdateToolCallUpdate{ToolCallID: id, Content: content}); err != nil {
		return "", content, err
	}

	exit, err := terminal.WaitForExit(ctx)
	if err != nil {
		_ = terminal.Kill(context.WithoutCancel(ctx)) // the turn was cancelled
		return "", content, err
	}
	output, err := terminal.CurrentOutput(ctx)
	if err != nil {
		return "", content, err
	}

	var result strings.Builder
	switch {
	case exit.ExitCode != nil:
		fmt.Fprintf(&result, "Exit code %d", *exit.ExitCode)
	case exit.Signal != nil:
		fmt.Fprintf(&result, "Killed by signal %s", *exit.Signal)
	}
	if output.Truncated {
		result.WriteString(", output truncated to the last part")
	}
	result.WriteString(":\n")
	result.WriteString(output.Output)
	if exit.ExitCode == nil || *exit.ExitCode != 0 {
		return result.String(), content, errors.New("command failed")
	}
	return result.String(), content, nil
}

var errRejected = errors.New("the user rejected this")

// allow asks the user whether the tool call may go ahead, showing content,
// and returns errRejected if not. Auto mode allows everything without asking.
func (a *openAgent) allow(ctx context.Context, stream *acp1.SessionStream, sess *session, id acp1.ToolCallID, title string, kind acp1.ToolKind, content []acp1.ToolCallContent, locations ...acp1.ToolCallLocation) error {
	if sess.currentMode() == autoMode {
		return nil
	}
	permission, err := a.client.RequestPermission(ctx, &acp1.RequestPermissionRequest{
		SessionID: stream.SessionID(),
		ToolCall: acp1.ToolCallUpdate{
			ToolCallID: id,
			Title:      &title,
			Kind:       &kind,
			Status:     new(acp1.ToolCallStatusPending),
			Locations:  locations,
			Content:    content,
		},
		Options: []acp1.PermissionOption{
			{OptionID: "allow", Name: "Allow", Kind: acp1.PermissionOptionKindAllowOnce},
			{OptionID: "reject", Name: "Reject", Kind: acp1.PermissionOptionKindRejectOnce},
		},
	})
	if err != nil {
		return err
	}
	// Anything but a selected "allow", including a cancelled request, rejects.
	if selected, ok := permission.Outcome.As[acp1.RequestPermissionOutcomeSelected](); ok && selected.OptionID == "allow" {
		return nil
	}
	return errRejected
}

// resolve makes a path from the model absolute, relative to the session's
// working directory; ACP file methods take absolute paths.
func (s *session) resolve(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(s.cwd, path)
}
