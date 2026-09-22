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
The root `acp` package implements ACP v1 on top of `schema/v1`; a v2 façade over `schema/v2`
is not built yet.

## Installation

```bash
go get github.com/ironpark/go-acp
```

## Example Code

See the [docs/example](./docs/example/) directory for complete working examples:

- **[Agent Example](./docs/example/agent/)** — sessions, streamed updates and a permission request
- **[Client Example](./docs/example/client/)** — spawning an agent and driving one prompt turn

## Architecture

- **`AgentSideConnection`** — serves an `Agent` and calls the peer client
- **`ClientSideConnection`** — serves a `Client` and calls the peer agent
- **`Transport`** — pluggable framing (stdio by default, HTTP+SSE included)
- **`SessionManager`** — session lifecycle methods backed by a `SessionStore`
- **`SessionStream`** — session updates without rebuilding the union by hand
- **`Middleware`** — composable wrappers around incoming requests and notifications
- **`TerminalHandle`** — terminal id and session id bound together
- **`schema/v1`** — generated wire types, unions and Zod-based validation

Incoming parameters are validated with the SDK's own Zod rules before a handler sees them,
and invalid ones are answered with `-32602` without invoking the handler.

## Quick Start

### Agent

```go
conn := acp.NewAgentSideConnection(func(c *acp.AgentSideConnection) acp.Agent {
    return &MyAgent{client: c} // the connection is also the peer Client
}, os.Stdin, os.Stdout)

if err := conn.Start(context.Background()); err != nil {
    log.Fatal(err)
}
```

`Agent` requires only `Initialize`, `Authenticate`, `NewSession`, `Prompt` and `Cancel`.
Everything else is an optional interface — implement `acp.SessionLoader`, `acp.SessionLister`,
`acp.SessionModeSetter`, `acp.NesHandler` and so on, and advertise the matching capability from
`Initialize`. Methods you do not implement are answered with `-32601`.

### Client

```go
conn, err := acp.SpawnAgent(ctx, func(*acp.ClientSideConnection) acp.Client {
    return &MyClient{}
}, "my-agent")
if err != nil {
    log.Fatal(err)
}
go conn.Start(ctx)

conn.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersion})
session, _ := conn.NewSession(ctx, &acp.NewSessionRequest{Cwd: cwd, MCPServers: []schema.MCPServer{}})
conn.Prompt(ctx, &acp.PromptRequest{SessionID: session.SessionID, Prompt: prompt})
```

`Client` requires only `SessionUpdate` and `RequestPermission`. File system, terminal and
elicitation support come from `acp.FileReader`, `acp.FileWriter`, `acp.TerminalHandler` and
`acp.ElicitationHandler`.

## Features

### Cancellation

Cancelling the context of an outgoing call sends `$/cancel_request` for that request id.
On the receiving side the matching handler's context is cancelled, and the peer gets
`-32800 Request cancelled` unless the handler answers first. This is separate from
`session/cancel`, which cancels a whole prompt turn.

### Transport Layer

```go
// Default: stdio (newline-delimited JSON)
conn := acp.NewAgentSideConnection(newAgent, os.Stdin, os.Stdout)

// HTTP+SSE for web deployments
transport := acp.NewHTTPServerTransport()
conn := acp.NewAgentSideConnection(newAgent, nil, nil, acp.WithTransport(transport))
http.Handle("/", transport.Handler())
```

### Middleware

```go
conn := acp.NewAgentSideConnection(newAgent, os.Stdin, os.Stdout,
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
manager := acp.NewSessionManager(
    acp.NewMemoryStore[*MySession](),
    func(ctx context.Context, params *acp.NewSessionRequest) (acp.SessionID, *MySession, error) {
        return acp.GenerateSessionID(), &MySession{cwd: params.Cwd}, nil
    },
)

type MyAgent struct {
    *acp.SessionManager[*MySession] // supplies NewSession, LoadSession, ListSessions, DeleteSession
}
```

Override any of those by declaring the method on the agent itself.

### SessionStream

```go
stream := acp.NewSessionStream(client, sessionID)

stream.SendText(ctx, "Hello!")
stream.SendThought(ctx, "thinking...")

stream.StartToolCall(ctx, toolID, "Reading file", schema.ToolKindRead)
stream.CompleteToolCall(ctx, toolID, content...)

stream.SendPlan(ctx, entries)
stream.Send(ctx, anyUpdate) // escape hatch for variants without a helper
```

### Unions

Generated unions wrap a sealed variant interface, so a type switch replaces the old matchers:

```go
switch update := notification.Update.Variant().(type) {
case schema.SessionUpdateAgentMessageChunk:
    if text, ok := update.Content.Variant().(schema.ContentBlockText); ok {
        fmt.Print(text.Text)
    }
case schema.SessionUpdateToolCall:
    fmt.Println(update.Title)
}

update := schema.NewSessionUpdate(schema.SessionUpdatePlan{Entries: entries})
```

Unknown tags round-trip unchanged through the union's `Custom` variant.

### Connection Options

```go
acp.NewAgentSideConnection(newAgent, os.Stdin, os.Stdout,
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
| `initialize`, `authenticate`, `session/new`, `session/prompt`, `session/cancel` | `Agent` (required) |
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

The `mcp/*` methods have no typed handler here, matching the reference SDKs; they arrive
through `ExtMethodHandler`. `$/cancel_request` is handled by the connection itself.

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
