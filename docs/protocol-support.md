# Protocol Support

[README](../README.md) | [SDK guide](guide.md) | [한국어](protocol-support.ko.md)

ACP protocol version 1, as pinned in [`schema/typescript/REVISION`](../schema/typescript/REVISION).

## Agent methods (client → agent)

| Method | Go interface |
| --- | --- |
| `initialize`, `session/new`, `session/prompt`, `session/cancel` | `Agent` (required) |
| `authenticate` | `Authenticator` |
| `session/load` | `SessionLoader` |
| `session/list` | `SessionLister` |
| `session/delete` | `SessionDeleter` |
| `session/fork` | `SessionForker` (unstable) |
| `session/resume` | `SessionResumer` |
| `session/close` | `SessionCloser` |
| `session/set_mode` | `SessionModeSetter` |
| `session/set_config_option` | `SessionConfigOptionSetter` |
| `providers/list`, `providers/set`, `providers/disable` | `ProviderManager` (unstable) |
| `logout` | `LogoutHandler` |
| `nes/*` | `NesHandler` (unstable) |
| `document/did*` | `DocumentHandler` (unstable) |
| `mcp/message` | `MCPMessageHandler` (unstable) |

## Client methods (agent → client)

| Method | Go interface |
| --- | --- |
| `session/update`, `session/request_permission` | `Client` (required) |
| `fs/read_text_file` | `FileReader` |
| `fs/write_text_file` | `FileWriter` |
| `terminal/*` | `TerminalHandler` |
| `elicitation/create`, `elicitation/complete` | `ElicitationHandler` |
| `mcp/connect`, `mcp/message`, `mcp/disconnect` | `MCPConnector` (unstable) |

`$/cancel_request` is handled by the connection itself.

## ACP v2 (`acp2`, draft)

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

A `session/cancel` sent after a prompt always reaches that prompt's turn. `ClientSession.Prompt`
returns only once the prompt is queued, so a `Cancel` after it follows it on the wire, and a cancel
that arrives before the agent's handler calls `BeginTurn` or `JoinTurn` still starts that turn
cancelled with `acp.ErrTurnCancelled`.
