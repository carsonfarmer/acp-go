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
errors and the session store. The protocol façades are versioned siblings: [`acpv1`](./acpv1/) on
`schema/v1` (stable) and [`acpv2`](./acpv2/) on `schema/v2` (draft, may change). [`router`](./router/)
serves both versions on one endpoint.

## Installation

```bash
go get github.com/ironpark/go-acp
```

## Example Code

See the [docs/example](./docs/example/) directory for complete working examples:

- **[Agent Example](./docs/example/agent/)** — sessions, streamed updates and a permission request
- **[Client Example](./docs/example/client/)** — spawning an agent and driving one prompt turn

## Architecture

- **`acp`** (root) — `Option`s, `Transport` (stdio, HTTP+SSE), `Middleware`, `RequestError`, `SessionStore`,
  `TurnTracker`, typed extensions (`CallExt`, `ExtRouter`)
- **`acpv1.AgentSideConnection`** — serves an `Agent` and calls the peer client
- **`acpv1.ClientSideConnection`** — serves a `Client` and calls the peer agent
- **`acpv1.SpawnAgent`**, **`acpv1.Pipe`** — an agent as a child process, or both sides in memory
- **`acpv1.ClientSession`**, **`acpv1.Turn`** — prompt a session and read that turn's updates
- **`acpv1.SessionManager`** — session lifecycle and turn cancellation backed by a store
- **`acpv1.SessionStream`** — session updates without rebuilding the union by hand
- **`acpv1.TerminalHandle`** — terminal id and session id bound together
- **`acpv1.CapabilitiesOf`** — capabilities derived from the interfaces an agent implements
- **`acpv2`** — the same façades for the draft ACP v2 (`schema/v2`)
- **`router.ProtocolRouter`** — one endpoint serving v1 and v2 agents; **`router.ClientConnector`** — the client side, v2 with v1 fallback
- **`schema/v1`, `schema/v2`** — generated wire types, unions and Zod-based validation

Incoming parameters are validated with the SDK's own Zod rules before a handler sees them,
and invalid ones are answered with `-32602` without invoking the handler.

## Quick Start

### Agent

```go
conn := acpv1.NewAgentSideConnection(func(c *acpv1.AgentSideConnection) acpv1.Agent {
    return &MyAgent{client: c} // the connection is also the peer Client
}, os.Stdin, os.Stdout)

if err := conn.Start(context.Background()); err != nil {
    log.Fatal(err)
}
```

`Agent` requires only `Initialize`, `NewSession`, `Prompt` and `Cancel`.
Everything else is an optional interface — implement `acpv1.Authenticator`, `acpv1.SessionLoader`, `acpv1.SessionLister`,
`acpv1.SessionModeSetter`, `acpv1.NesHandler` and so on. Methods you do not implement are answered
with `-32601`. `acpv1.CapabilitiesOf(agent)` returns the capabilities those interfaces imply, so the
`Initialize` response cannot advertise a method the connection would reject:

```go
caps := acpv1.CapabilitiesOf(a)
caps.PromptCapabilities = &schema.PromptCapabilities{Image: new(true)} // content capabilities are yours to set
return &acpv1.InitializeResponse{ProtocolVersion: acpv1.ProtocolVersion, AgentCapabilities: caps}, nil
```

### Client

```go
client := &MyClient{}
agent, err := acpv1.SpawnAgent(ctx, exec.Command("my-agent"), func(*acpv1.ClientSideConnection) acpv1.Client {
    return client
})
if err != nil {
    log.Fatal(err)
}
defer agent.Close()

agent.Initialize(ctx, &acpv1.InitializeRequest{
    ProtocolVersion:    acpv1.ProtocolVersion,
    ClientCapabilities: acpv1.ClientCapabilitiesOf(client),
})
session, _ := agent.StartSession(ctx, &acpv1.NewSessionRequest{Cwd: cwd})

turn, _ := session.Prompt(ctx, acpv1.TextBlock("Summarize README.md"))
for update := range turn.Updates() {
    render(update) // tool calls, plans, message chunks...
}
response, err := turn.Wait() // or: text, err := turn.Text()
```

`SpawnAgent` already runs the read loop; `agent.Wait()` reports how the process and connection
ended. The agent's stderr goes to the parent's unless `cmd.Stderr` is set. `acpv1.Pipe` connects
an agent and a client in memory, which is handy in tests.

`Client` requires only `SessionUpdate` and `RequestPermission`. `SessionUpdate` sees every update,
including those outside a `Turn`; notifications are handled in order on the read loop, so a handler
must not wait on a call to the agent. File system, terminal and elicitation support come from
`acpv1.FileReader`, `acpv1.FileWriter`, `acpv1.TerminalHandler` and `acpv1.ElicitationHandler`;
`acpv1.ClientCapabilitiesOf(client)` derives the matching flags.

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
notifications are ignored. Every `_meta` field is a `schema.Meta`, which keeps values as raw JSON:

```go
var meta schema.Meta
meta.Set("trace", Trace{ID: "abc"})
trace, ok, err := params.Meta.Get[Trace]("trace")
```

### Serving v1 and v2 together

```go
r := router.New().
    WithV1(func(c *acpv1.AgentSideConnection) acpv1.Agent { return &v1Agent{client: c} }).
    WithV2(func(c *acpv2.AgentSideConnection) acpv2.Agent { return &v2Agent{client: c} })
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
    WithV1(newV1Client, &acpv1.InitializeRequest{ClientCapabilities: v1Caps}).
    WithV2(newV2Client, &acpv2.InitializeRequest{Info: info}).
    Spawn(ctx, func() *exec.Cmd { return exec.Command("my-agent") })
defer agent.Close() // Close, Wait, Done and extension calls work on either version
if agent.V2 != nil {
    // agent.V2, agent.V2Init
} else {
    // agent.V1, agent.V1Init
}
```

### Transport Layer

```go
// Default: stdio (newline-delimited JSON)
conn := acpv1.NewAgentSideConnection(newAgent, os.Stdin, os.Stdout)

// HTTP+SSE for web deployments
transport := acp.NewHTTPServerTransport()
conn := acpv1.NewAgentSideConnection(newAgent, nil, nil, acp.WithTransport(transport))
http.Handle("/", transport.Handler())
```

### Middleware

```go
conn := acpv1.NewAgentSideConnection(newAgent, os.Stdin, os.Stdout,
    acp.WithMiddleware(
        acp.LoggingMiddleware(logger.Printf),    // log methods and durations
        acp.TimeoutMiddleware(30*time.Second),   // per-handler timeout
    ),
)
```

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
manager := acpv1.NewSessionManager(
    acpv1.NewMemoryStore[*MySession](),
    func(ctx context.Context, params *acpv1.NewSessionRequest) (acpv1.SessionID, *MySession, error) {
        return acpv1.GenerateSessionID(), &MySession{cwd: params.Cwd}, nil
    },
)

type MyAgent struct {
    *acpv1.SessionManager[*MySession] // NewSession, Cancel, LoadSession, ListSessions, DeleteSession
}

func (a *MyAgent) Prompt(ctx context.Context, params *acpv1.PromptRequest) (*acpv1.PromptResponse, error) {
    ctx, done, err := a.BeginTurn(ctx, params.SessionID) // the manager's Cancel cancels ctx
    if err != nil {
        return nil, err // acp.ErrTurnInProgress: v1 runs one turn per session
    }
    defer done()
    if err := a.work(ctx); context.Cause(ctx) == acp.ErrTurnCancelled {
        return &acpv1.PromptResponse{StopReason: schema.StopReasonCancelled}, nil
    } else if err != nil {
        return nil, err
    }
    return &acpv1.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil
}
```

Override any of those by declaring the method on the agent itself. `acp.SessionStore[ID, T]`,
`acp.MemoryStore` and `acp.TurnTracker` are the version-neutral building blocks; each façade
aliases the stores with its own session id.

### SessionStream

```go
stream := acpv1.NewSessionStream(client, sessionID)

stream.SendText(ctx, "Hello!")
stream.SendThought(ctx, "thinking...")

stream.StartToolCall(ctx, toolID, "Reading file", schema.ToolKindRead)
stream.CompleteToolCall(ctx, toolID, acpv1.ToolText(contents))

stream.SendPlan(ctx, entries)
stream.Send(ctx, schema.SessionUpdateSessionInfoUpdate{Title: new("Refactor")}) // variants without a helper
```

`acpv1.TextBlock`, `acpv1.TextOf`, `acpv1.Texts` (an iterator over a prompt's text blocks) and
`acpv1.ToolText` cover the common text content. The v2
`SessionStream` takes a message id on every message and adds `Running`, `RequiresAction` and
`Idle` for the explicit turn state.

### Unions

Generated unions wrap a sealed variant interface, so a type switch replaces the old matchers:

```go
switch update := notification.Update.Variant().(type) {
case schema.SessionUpdateAgentMessageChunk:
    if text, ok := acpv1.TextOf(update.Content); ok {
        fmt.Print(text)
    }
case schema.SessionUpdateToolCall:
    fmt.Println(update.Title)
}

update := schema.NewSessionUpdate(schema.SessionUpdatePlan{Entries: entries})
```

A tag this SDK does not know never fails the message. It decodes into the union's
`Custom` variant where the schema defines one, and otherwise into a generated `…Unknown`
variant (such as `schema.SessionUpdateUnknown`) whose `Raw` field holds the object as
received and is encoded unchanged. Handle it in a `default` case, or ignore it as the
protocol recommends.

### Connection Options

```go
acpv1.NewAgentSideConnection(newAgent, os.Stdin, os.Stdout,
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

### ACP v2 (`acpv2`, draft)

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
