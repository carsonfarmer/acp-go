![agent client protocol golang banner](./docs/imgs/banner-dark.jpg)

# Agent Client Protocol - Go Implementation

A Go implementation of the Agent Client Protocol (ACP), which standardizes communication between _code editors_ (interactive programs for viewing and editing source code) and _coding agents_ (programs that use generative AI to autonomously modify code).

This is an **unofficial** implementation of the ACP specification in Go. The official protocol specification and reference implementations can be found at the [official repository](https://github.com/zed-industries/agent-client-protocol).

> [!NOTE]
> The Agent Client Protocol is under active development. This implementation may lag behind the latest specification changes. Please refer to the [official repository](https://github.com/zed-industries/agent-client-protocol) for the most up-to-date protocol specification.

Learn more about the protocol at [agentclientprotocol.com](https://agentclientprotocol.com/).

## `next` branch

This branch requires Go 1.27+ and is a rebuild of the SDK on `encoding/json/v2`.
Wire types are generated from the official TypeScript SDK with `go-tree-sitter` — see
[schema generation](schema/README.md) for inputs, regeneration and current limits.
The root `acp` package holds what every protocol version shares — options, transports, middleware,
errors and the session store. The protocol façades are versioned siblings: [`acp1`](./acp1/) on
`schema/v1` (stable) and [`acp2`](./acp2/) on `schema/v2` (draft, may change). [`router`](./router/)
serves both versions on one endpoint.

## Installation

```bash
go get github.com/ironpark/go-acp
```

## Example Code

See the [examples](./examples/) directory for complete working examples:

- **[Echo Agent](./examples/echo/)** — the smallest agent: the four required methods
- **[Agent](./examples/agent/)** — sessions, modes, cancellation, a plan, tool calls with a terminal and a diff, a permission request, an extension method
- **[Client](./examples/client/)** — an interactive client for any stdio agent: Ctrl-C cancellation, mode switching, terminals, file methods
- **[Open Agent](./examples/open-agent/)** — a coding agent driven by a model on OpenRouter: streaming, tool calls through the client's files and terminal, permission prompts
- **[HTTP Agent](./examples/http-agent/) / [HTTP Client](./examples/http-client/)** — the same connection over Streamable HTTP or WebSocket, with a bearer token and a reconnect that loads the session
- **[Dual Agent](./examples/dual-agent/) / [Dual Client](./examples/dual-client/)** — v1 and v2 on one endpoint, a client that falls back from v2 to v1, and v2 session resume with replay
- **[In-process](./examples/inprocess/)** — an agent and a client in one process, connected with `acp1.Pipe`
- **[MCP over ACP](./acpmcp/)** (unstable) — MCP servers provided by the client and called over the ACP connection, in the separate `acpmcp` module

## Architecture

- **`acp`** (root) — `Option`s, `Transport` (stdio, Streamable HTTP, WebSocket), `Middleware`, `RequestError`, `SessionStore`,
  `TurnTracker`, typed extensions (`CallExt`, `ExtRouter`)
- **`acp1.AgentSideConnection`** — serves an `Agent` and calls the peer client
- **`acp1.ClientSideConnection`** — serves a `Client` and calls the peer agent
- **`acp1.SpawnAgent`**, **`acp1.Pipe`** — an agent as a child process, or both sides in memory
- **`acp1.ClientSession`**, **`acp1.Turn`** — prompt a session and read that turn's updates
- **`acp1.SessionManager`** — session lifecycle and turn cancellation backed by a store
- **`acp1.SessionStream`** — session updates without rebuilding the union by hand
- **`acp1.TerminalHandle`** — terminal id and session id bound together
- **`acp1.CapabilitiesOf`** — capabilities derived from the interfaces an agent implements
- **`acp2`** — the same façades for the draft ACP v2 (`schema/v2`)
- **`router.ProtocolRouter`** — one endpoint serving v1 and v2 agents; **`router.ClientConnector`** — the client side, v2 with v1 fallback
- **`acpmcp`** (separate module, unstable) — MCP-over-ACP on top of the MCP Go SDK: `HostV1`/`HostV2` serve a client's MCP servers, `DialerV1`/`DialerV2` connect an agent to them
- **`schema/v1`, `schema/v2`** — generated wire types, unions and Zod-based validation

Incoming parameters are validated with the SDK's own Zod rules before a handler sees them,
and invalid ones are answered with `-32602` without invoking the handler.

## Quick Start

### Agent

```go
conn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
    return &MyAgent{client: c} // the connection is also the peer Client
}, os.Stdin, os.Stdout)

if err := conn.Start(context.Background()); err != nil {
    log.Fatal(err)
}
```

`Agent` requires only `Initialize`, `NewSession`, `Prompt` and `Cancel`.
Everything else is an optional interface — implement `acp1.Authenticator`, `acp1.SessionLoader`, `acp1.SessionLister`,
`acp1.SessionModeSetter`, `acp1.NesHandler` and so on. Methods you do not implement are answered
with `-32601`. `acp1.CapabilitiesOf(agent)` returns the capabilities those interfaces imply, so the
`Initialize` response cannot advertise a method the connection would reject:

```go
caps := acp1.CapabilitiesOf(a)
caps.PromptCapabilities = &schema.PromptCapabilities{Image: new(true)} // content capabilities are yours to set
return &acp1.InitializeResponse{ProtocolVersion: acp1.ProtocolVersion, AgentCapabilities: caps}, nil
```

### Client

```go
client := &MyClient{}
agent, err := acp1.SpawnAgent(ctx, exec.Command("my-agent"), func(*acp1.ClientSideConnection) acp1.Client {
    return client
})
if err != nil {
    log.Fatal(err)
}
defer agent.Close()

agent.Initialize(ctx, &acp1.InitializeRequest{ // a zero ProtocolVersion sends acp1.ProtocolVersion
    ClientCapabilities: acp1.ClientCapabilitiesOf(client),
})
session, _ := agent.StartSession(ctx, &acp1.NewSessionRequest{Cwd: cwd})

turn, _ := session.Prompt(ctx, acp1.TextBlock("Summarize README.md"))
for update := range turn.Updates() {
    render(update) // tool calls, plans, message chunks...
}
response, err := turn.Wait() // or: text, response, err := turn.Text()
```

`SpawnAgent` already runs the read loop; `agent.Wait()` reports how the process and connection
ended. The agent's stderr goes to the parent's unless `cmd.Stderr` is set. `acp1.Pipe` connects
an agent and a client in memory, which is handy in tests.

`Client` requires only `SessionUpdate` and `RequestPermission`; a client whose agent never asks for
permission can embed `acp1.UnimplementedClient` for both. `SessionUpdate` sees every update,
including those outside a `Turn`; notifications are handled in order on the read loop, so a handler
must not wait on a call to the agent. File system, terminal and elicitation support come from
`acp1.FileReader`, `acp1.FileWriter`, `acp1.TerminalHandler` and `acp1.ElicitationHandler`;
`acp1.ClientCapabilitiesOf(client)` derives the matching flags.

## Features

### Cancellation

Cancelling the context of an outgoing call sends `$/cancel_request` for that request id.
On the receiving side the matching handler's context is cancelled, and the peer gets
`-32800 Request cancelled` unless the handler answers first. This is separate from
`session/cancel`, which cancels a whole prompt turn (see [Sessions](#sessions)).

### Errors

Return an `acp.Err…` constructor from a handler to choose the code the peer receives; any other
error becomes `-32603`. Errors from the peer come back as `*acp.RequestError`:

```go
if acp.IsCode(err, acp.ErrorCodeAuthRequired) {
    // authenticate, then retry
}
```

### Extensions and `_meta`

```go
type MyAgent struct {
    acp.ExtRouter // implements ExtMethodHandler and ExtNotificationHandler
    // ...
}

a.HandleExt("_example.com/index", func(ctx context.Context, p *IndexParams) (*IndexResult, error) {
    return &IndexResult{Files: 42}, nil
})

result, err := acp.CallExt[IndexResult](ctx, conn, "_example.com/index", IndexParams{Path: "."})
```

Params that fail to decode get `-32602`, unregistered methods `-32601`, and unregistered
notifications are ignored. Every `_meta` field is an `acp1.Meta`, which keeps values as raw JSON:

```go
var meta acp1.Meta
meta.Set("trace", Trace{ID: "abc"})
trace, ok, err := params.Meta.Get[Trace]("trace")
```

### Serving v1 and v2 together

```go
r := router.New().
    WithV1(func(c *acp1.AgentSideConnection) acp1.Agent { return &v1Agent{client: c} }).
    WithV2(func(c *acp2.AgentSideConnection) acp2.Agent { return &v2Agent{client: c} })
err := r.ServeStdio(ctx, os.Stdin, os.Stdout)
```

The router reads the first message, which must be `initialize`, picks the highest configured
version not above the one requested, rewrites only the initialize params (a v2 request routed to
a v1-only agent is downgraded: `info` → `clientInfo`, no `fs`/`terminal`), and forwards everything
after that unchanged. Options, transports and middleware live in the root `acp` package, so one
value configures either façade.

A client that supports both versions uses `router.NewClient`. It spawns the agent, initializes
with v2, and if the agent answers `protocolVersion` 1 (or rejects the v2 request) restarts it
with v1, so each version sends its own initialize request:

```go
agent, err := router.NewClient().
    WithV1(newV1Client, &acp1.InitializeRequest{ClientCapabilities: v1Caps}).
    WithV2(newV2Client, &acp2.InitializeRequest{Info: info}).
    Spawn(ctx, func() *exec.Cmd { return exec.Command("my-agent") })
defer agent.Close() // Close, Wait, Done and extension calls work on either version
if agent.V2 != nil {
    // agent.V2, agent.V2Init
} else {
    // agent.V1, agent.V1Init
}
```

For a remote agent, `Connect` takes a dial function instead, called once per attempt:

```go
agent, err := router.NewClient().WithV1(…).WithV2(…).
    Connect(ctx, func(ctx context.Context) (acp.Transport, error) {
        return acp.NewHTTPClientTransport("https://host/acp"), nil // or acp.DialWebSocket
    })
```

### Transport Layer

```go
// Default: stdio (newline-delimited JSON)
conn := acp1.NewAgentSideConnection(newAgent, os.Stdin, os.Stdout)

// Streamable HTTP and WebSocket for remote agents: one connection, and one agent, per client
server := acp.NewHTTPServer(func(ctx context.Context, t acp.Transport) error {
    return acp1.NewAgentSideConnection(newAgent, nil, nil, acp.WithTransport(t)).Start(ctx)
})
http.Handle("/acp", server)

// The client side of any transport
agent := acp1.ConnectAgent(ctx, acp.NewHTTPClientTransport("https://host/acp"), newClient)
defer agent.Close() // also ends the connection on the server

ws, err := acp.DialWebSocket(ctx, "wss://host/acp") // the same endpoint over WebSocket
agent := acp1.ConnectAgent(ctx, ws, newClient)
```

Both follow the draft RFD the TypeScript and Python SDKs implement, and interoperate with them.
Streamable HTTP uses `POST` for client messages (`initialize` answers with an
`Acp-Connection-Id`) and Server-Sent Events streams for the agent's, one per connection and one
per session. A `GET` with `Upgrade: websocket` on the same endpoint carries the whole connection
as text frames instead. WebSockets from browser pages on other origins are refused unless
`acp.WithWebSocketOrigins` allows them.

Reconnecting is a new connection, as in the other SDKs: dial again with the same headers and
`acp.WithCookieJar(jar)`, so a load balancer's affinity cookie routes the client back, then
`Initialize` and `LoadSession` the saved session id if the agent advertises `loadSession`.
Messages sent while the client was away are not replayed; the protocol leaves that to v2.
The `http-client` example shows the flow with `-reconnect`.

Short drops are handled below that. The HTTP client reopens a dropped event stream, backing off,
until the server answers that the connection is gone; the server hands a stream to the newer `GET`
and resends a message whose write failed. The server ends a connection whose client has had no
stream open for five minutes (`acp.WithIdleTimeout`), and both ends of a WebSocket ping every 15
seconds and close it when the peer stops answering (`acp.WithWebSocketPing` on the server,
`acp.WithPingInterval` on the client).

### Middleware

```go
conn := acp1.NewAgentSideConnection(newAgent, os.Stdin, os.Stdout,
    acp.WithMiddleware(
        acp.LoggingMiddleware(slog.Default()),   // log methods, durations and errors
        acp.TimeoutMiddleware(30*time.Second),   // per-handler timeout
    ),
)
```

`LoggingMiddleware` logs requests at Info and notifications, such as the frequent session
updates, at Debug, with `method` and `duration` attributes; a failure logs at Warn with `error`
and the JSON-RPC `code`.

Panics in handlers are already recovered by the connection and reported as `-32603`,
so no recovery middleware is needed. Custom middleware wraps either direction:

```go
authenticated := acp.Middleware{
    Request: func(next acp.RequestHandler) acp.RequestHandler {
        return func(ctx context.Context, method string, params jsontext.Value) (any, error) {
            if method != schema.AgentMethodsInitialize && !isAuthenticated(ctx) {
                return nil, acp.ErrAuthRequired(nil)
            }
            return next(ctx, method, params)
        }
    },
}
```

### Sessions

```go
manager := acp1.NewSessionManager(
    acp1.NewMemoryStore[*MySession](),
    func(ctx context.Context, params *acp1.NewSessionRequest) (acp1.SessionID, *MySession, error) {
        return acp1.GenerateSessionID(), &MySession{cwd: params.Cwd}, nil
    },
)

type MyAgent struct {
    *acp1.SessionManager[*MySession] // NewSession, Cancel, LoadSession, ListSessions, DeleteSession, ResumeSession, CloseSession
}

func (a *MyAgent) Prompt(ctx context.Context, params *acp1.PromptRequest) (*acp1.PromptResponse, error) {
    ctx, done, err := a.BeginTurn(ctx, params.SessionID) // the manager's Cancel cancels ctx
    if err != nil {
        return nil, err // acp.ErrTurnInProgress: v1 runs one turn per session
    }
    defer done()
    if err := a.work(ctx); context.Cause(ctx) == acp.ErrTurnCancelled {
        return &acp1.PromptResponse{StopReason: acp1.StopReasonCancelled}, nil
    } else if err != nil {
        return nil, err
    }
    return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
}
```

Override any of those by declaring the method on the agent itself; the manager checks that a loaded
or resumed session exists, and replaying its history is the agent's job. `Lookup(id)` returns a
session's state or the resource-not-found error to return as is. `CloseSession` cancels the
running turn and keeps the session to resume; `DeleteSession` removes it. `acp2.SessionManager`
serves the v2 session baseline the same way. `acp.SessionStore[ID, T]`,
`acp.MemoryStore` and `acp.TurnTracker` are the version-neutral building blocks; each façade
aliases the stores with its own session id.

### SessionStream

```go
stream := acp1.NewSessionStream(client, sessionID)

stream.SendText(ctx, "Hello!")
stream.SendThought(ctx, "thinking...")

stream.StartToolCall(ctx, toolID, "Reading file", acp1.ToolKindRead)
stream.CompleteToolCall(ctx, toolID, acp1.ToolText(contents))
stream.CompleteToolCall(ctx, editID, acp1.ToolDiff(path, &oldText, newText))
stream.CompleteToolCall(ctx, runID, acp1.ToolTerminal(terminal.ID)) // terminal from conn.NewTerminal

stream.SendPlan(ctx, entries)
stream.Send(ctx, acp1.SessionUpdateSessionInfoUpdate{Title: new("Refactor")}) // variants without a helper
stream.WithMeta(meta).SendText(ctx, "…")                                       // _meta on each notification
```

`acp1.TextBlock`, `acp1.TextOf`, `acp1.Texts` (an iterator over a prompt's text blocks),
`acp1.JoinTexts` (their concatenation) and
`acp1.ToolText` cover the common text content, and `acp1.ToolDiff` and `acp1.ToolTerminal` the
other tool output. Tool call ids must be unique within a session; `acp1.GenerateToolCallID` and
`acp1.GenerateMessageID` mint random ones, like `GenerateSessionID`. The v2
`SessionStream` takes a message id on every message and adds `Running`, `RequiresAction` and
`Idle` for the explicit turn state.

### Unions

Generated unions wrap a sealed variant interface, so a type switch replaces the old matchers:

```go
switch update := notification.Update.Variant().(type) {
case acp1.SessionUpdateAgentMessageChunk:
    if text, ok := acp1.TextOf(update.Content); ok {
        fmt.Print(text)
    }
case acp1.SessionUpdateToolCall:
    fmt.Println(update.Title)
}

update := acp1.NewSessionUpdate(acp1.SessionUpdatePlan{Entries: entries})
```

A tag this SDK does not know never fails the message. It decodes into the union's
`Custom` variant where the schema defines one, and otherwise into a generated `…Unknown`
variant (such as `acp1.SessionUpdateUnknown`) whose `Raw` field holds the object as
received and is encoded unchanged. Handle it in a `default` case, or ignore it as the
protocol recommends.

### Optional Fields

Optional fields are pointers (`omitzero`), so an explicit `false` or `""` survives encoding. Every
pointer field also has a nil-safe getter, protobuf style: a pointer to a struct comes back as is, so
calls chain through absent objects, and any other pointer is dereferenced, giving the zero value when
the field or the receiver is nil:

```go
if init.GetAgentCapabilities().GetMCPCapabilities().GetACP() { ... }
title := params.ToolCall.GetTitle() // "" when absent
```

Read the field itself when absence means something the zero value does not: a terminal's `nil`
`ExitCode` means a signal ended it, not exit code 0.

### Connection Options

```go
acp1.NewAgentSideConnection(newAgent, os.Stdin, os.Stdout,
    acp.WithWriteQueueSize(500),               // outgoing queue depth
    acp.WithRequestTimeout(30*time.Second),    // default deadline for outgoing calls
    acp.WithShutdownTimeout(10*time.Second),   // bound Close on in-flight handlers
    acp.WithErrorHandler(func(err error) {}),  // non-fatal errors
)
```

## Protocol Support

ACP protocol version 1, as pinned in [`schema/typescript/REVISION`](schema/typescript/REVISION).

### Agent methods (client → agent)

| Method | Go interface |
| --- | --- |
| `initialize`, `session/new`, `session/prompt`, `session/cancel` | `Agent` (required) |
| `authenticate` | `Authenticator` |
| `session/load` | `SessionLoader` |
| `session/list` | `SessionLister` |
| `session/delete` | `SessionDeleter` |
| `session/fork` | `SessionForker` (unstable) |
| `session/resume` | `SessionResumer` (unstable) |
| `session/close` | `SessionCloser` (unstable) |
| `session/set_mode` | `SessionModeSetter` |
| `session/set_config_option` | `SessionConfigOptionSetter` |
| `providers/list`, `providers/set`, `providers/disable` | `ProviderManager` (unstable) |
| `logout` | `LogoutHandler` |
| `nes/*` | `NesHandler` (unstable) |
| `document/did*` | `DocumentHandler` (unstable) |

### Client methods (agent → client)

| Method | Go interface |
| --- | --- |
| `session/update`, `session/request_permission` | `Client` (required) |
| `fs/read_text_file` | `FileReader` |
| `fs/write_text_file` | `FileWriter` |
| `terminal/*` | `TerminalHandler` |
| `elicitation/create`, `elicitation/complete` | `ElicitationHandler` |

The `mcp/*` methods have no typed handler in v1, matching the reference SDKs; they arrive
through `ExtMethodHandler`. `$/cancel_request` is handled by the connection itself.

### ACP v2 (`acp2`, draft)

| Method | Go interface |
| --- | --- |
| `initialize`, `session/new`, `session/prompt`, `session/cancel` | `Agent` (required) |
| `auth/login`, `auth/logout` | `AuthHandler` |
| `session/list`, `session/delete`, `session/fork`, `session/resume`, `session/close` | `SessionLister`, `SessionDeleter`, `SessionForker`, `SessionResumer`, `SessionCloser` |
| `session/set_config_option` | `SessionConfigOptionSetter` |
| `providers/*` | `ProviderManager` (unstable) |
| `nes/*`, `document/did*` | `NesHandler`, `DocumentHandler` (unstable) |
| `mcp/message` (agent side) | `MCPMessageHandler` (unstable) |
| `session/update`, `session/request_permission` | `Client` (required) |
| `mcp/connect`, `mcp/message`, `mcp/disconnect` | `MCPConnector` (unstable) |
| `elicitation/create`, `elicitation/complete` | `ElicitationHandler` |

v2 has no `fs/*` or `terminal/*` methods — file and shell access go through MCP — so
`TerminalHandle` exists only in the v1 package. In v2 the prompt response only accepts the message;
a `Turn` ends when the agent reports the idle state.

Overlapping prompts follow each version's rules on both sides. A v1 session runs one turn at a
time: `ClientSession.Prompt` and `SessionManager.BeginTurn` both refuse a second prompt with
`acp.ErrTurnInProgress` (`-32600`). In v2 a prompt may contribute to running work: `Prompt`
returns `(*Turn, MessageID, error)` once the message is accepted and joins the running turn, and
`SessionManager.JoinTurn` hands the agent the running turn's context.

## Contributing

This is an unofficial implementation. For protocol specification changes, please contribute to the [official repository](https://github.com/zed-industries/agent-client-protocol).

For Go implementation issues and improvements, please open an issue or pull request.

## License

This implementation follows the same license as the official ACP specification.

## Related Projects

- **Official ACP Repository**: [zed-industries/agent-client-protocol](https://github.com/zed-industries/agent-client-protocol)
- **Rust Implementation**: Part of the official repository
- **Protocol Documentation**: [agentclientprotocol.com](https://agentclientprotocol.com/)

### Editors with ACP Support

- [Zed](https://zed.dev/docs/ai/external-agents)
- [neovim](https://neovim.io) through the [CodeCompanion](https://github.com/olimorris/codecompanion.nvim) plugin
- [yetone/avante.nvim](https://github.com/yetone/avante.nvim): A Neovim plugin designed to emulate the behaviour of the Cursor AI IDE
