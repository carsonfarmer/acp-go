# ACP Go Examples

Runnable examples for the [ACP Go SDK](https://github.com/ironpark/acp-go), from the smallest agent to a
different transport. Run each from the repository root.

| Example | Shows | Run |
|---|---|---|
| [`echo`](./echo/main.go) | The smallest agent: the four required methods, streaming each prompt back | `go run ./docs/example/echo` |
| [`agent`](./agent/main.go) | A complete agent: `SessionManager` sessions and cancellation, `SessionStream` tool calls, a permission request, an `ExtRouter` extension method, logging middleware | `go run ./docs/example/agent` |
| [`client`](./client/main.go) | An interactive client for any stdio agent: `SpawnAgent`, `ClientSession`/`Turn`, rendering updates, Ctrl-C cancellation, permission prompts, file system methods, `CallExt` | `go run ./docs/example/client [agent command...]` |
| [`http-agent`](./http-agent/main.go) | The echo agent served over Streamable HTTP and WebSocket on one endpoint with `acp.HTTPServer` | `go run ./docs/example/http-agent` |
| [`http-client`](./http-client/main.go) | One prompt turn against `http-agent` with `ConnectAgent`, over Streamable HTTP or, with `-ws`, WebSocket | `go run ./docs/example/http-client [-ws]` |
| [`dual-agent`](./dual-agent/main.go) | One binary serving ACP v1 and the draft v2 through `router.ProtocolRouter`, including the v2 prompt lifecycle | `go run ./docs/example/dual-agent` |
| [`dual-client`](./dual-client/main.go) | `router.ClientConnector`: v2 when the agent supports it, v1 otherwise | `go run ./docs/example/dual-client [agent command...]` |

## Agent and client together

With no arguments, the client builds the `agent` example and talks to it:

```sh
go run ./docs/example/client
```

Type a message to start a turn, answer the permission prompt, press Ctrl-C to cancel a running turn, or send
`/ping hello` to call the agent's `_example.com/ping` extension method. Ctrl-D quits. Pass `-v` to see the agent's
logs.

Any other stdio agent works too; give its command after the flags:

```sh
go build -o /tmp/echo ./docs/example/echo
go run ./docs/example/client /tmp/echo
```

## Over HTTP

The agent speaks Streamable HTTP, the remote transport of the TypeScript and Python SDKs, so their clients work
with it too. Start the agent, then run the client in another terminal:

```sh
go run ./docs/example/http-agent
go run ./docs/example/http-client      # Streamable HTTP
go run ./docs/example/http-client -ws  # WebSocket
```

## v1 and v2 together

With no arguments, the dual client builds `dual-agent` and negotiates v2. Point it at a v1-only agent and it
restarts the agent with v1:

```sh
go run ./docs/example/dual-client            # negotiated v2
go build -o /tmp/echo ./docs/example/echo
go run ./docs/example/dual-client /tmp/echo  # negotiated v1
```

The v1 `client` example works against `dual-agent` too; the router gives it the v1 agent.

## Agent by itself

An agent reads JSON-RPC from stdin and writes to stdout, so you can drive one by hand:

```sh
go run ./docs/example/agent
```

Paste this and press <kbd>enter</kbd>:

```json
{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":1}}
```

It answers with its capabilities (its request log goes to stderr):

```json
{"jsonrpc":"2.0","id":0,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"sessionCapabilities":{"list":{},"delete":{}}},"agentInfo":{"name":"example-agent","version":"0.1.0"}}}
```

From there, try [creating a session](https://agentclientprotocol.com/protocol/session-setup#creating-a-session) and
[sending a prompt](https://agentclientprotocol.com/protocol/prompt-turn#1-user-message).

## In Zed

[`agent/main.go`](./agent/main.go) is a compliant [ACP](https://agentclientprotocol.com) agent, so an ACP client like [Zed](https://zed.dev) can connect to it. The `echo` example works the same way; point the path at `docs/example/echo` instead.

1. Clone this repo

```sh
$ git clone https://github.com/ironpark/acp-go.git
```

2. Add the following at the root of your [Zed](https://zed.dev) settings:
> [!NOTE]
> Run the `agent: open settings` action from the command palette (<kbd>⌘⇧P</kbd> on macOS, <kbd>ctrl-shift-p</kbd> on Windows/Linux) 
```json
  "agent_servers": {
    "Example Agent": {
      "command": "go",
      "args": [
        "run",
        "-C",
        "/path/to/go-acp/docs/example/agent",
        "."
      ],
      "env": {}
  }
```

> [!NOTE]
>  Make sure to replace `/path/to/go-acp/docs/example/agent` with the path to your clone of this repository.


3. Run the `dev: open acp logs` action from the command palette (<kbd>⌘⇧P</kbd> on macOS, <kbd>ctrl-shift-p</kbd> on Windows/Linux) to see the messages exchanged between the example agent and Zed.

4. Then open the Agent Panel, and click "New Example Agent Thread" from the `+` menu on the top-right.

![Agent menu](../imgs/menu.png)

5. Finally, send a message and see the Agent respond!

![Final state](../imgs/final.png)

