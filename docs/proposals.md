# Improvement proposals for acp-go

These proposals come from comparing acp-go (at upstream `74e8b95`) with
[carsonfarmer/go-acp-sdk](https://github.com/carsonfarmer/go-acp-sdk) (at `2697f49`) and from
reviewing acp-go itself. Each one is small, keeps `acp1` and `acp2` symmetric, and fits the existing
split between generated and hand-written code.

**Source** names where each idea comes from:
- **go-acp-sdk**: an idea taken from go-acp-sdk.
- **review**: something found while reading acp-go.

Findings come from reading the code. One scratch program checked the JSON v2 and timestamp behaviour
cited in C1 and C4. The repo's own test suite was not run.

## Where changes land

| Change | Where |
| --- | --- |
| Routing and dispatch shape | The emitter in `internal/cmd/schema/facade/spec.go`, then `go generate ./...` |
| Method grouping and docs | The method tables in `internal/cmd/schema/facade/{v1,v2}.go`. `Via` and `CallVia` let a hand-written method run before a handler or call. |
| Wire-type corrections | `internal/cmd/schema/overrides.yaml` |
| Version-neutral runtime | Root `acp` package; each façade adds aliases or thin wrappers |
| Façade helpers | Hand-written files in `acp1/` and `acp2/`, kept in step |

Never edit `*.gen.go`. `go run . -check` in `internal/cmd/schema` catches stale output.

---

## Correctness

### C1. Answer messages that fail to decode instead of dropping them
*Source: go-acp-sdk + review. Size: S.*

**Problem.** `readLoop` decodes every message strictly with JSON v2 and only logs a failure
(`internal/jsonrpc/conn.go:274`). A request that fails this decode never gets a response, and its
sender waits forever unless it set a timeout. Two inputs confirmed to fail:
- A lone surrogate anywhere in the message, even deep inside `params`, such as `"text":"\ud83d"`.
  `JSON.stringify` produces this in TypeScript peers when a string is cut in the middle of an emoji.
- A duplicate member name.

Also, the `if len(msg.ID) > 0` branch at `conn.go:297` can never run.

go-acp-sdk replies `-32700` with a null id for JSON it cannot parse, and `-32600` for a wrong
`jsonrpc` version.

**Proposal.**
1. When decoding fails, decode only `id` again with lenient `jsontext` options
   (`AllowDuplicateNames`, `AllowInvalidUTF8`).
2. Reply `-32700 Parse error` if the JSON cannot be parsed, or `-32600 Invalid request` if it parses
   but is not a valid request. Use the recovered id, or `null` if there is none.
3. Separately, decide whether to accept lone surrogates in `params` and let the Zod validation step
   decide. Today the whole envelope is rejected before validation runs.
4. Remove the dead branch.
5. Add tests in `internal/jsonrpc/protocol_test.go`.

### C2. Respect the caller's context when the write queue is full
*Source: review. Size: S.*

**Problem.** `send` waits on only two things: space in `writeQueue`, or the connection closing
(`conn.go:340-351`). `SendNotification` checks the caller's `ctx` once, before queuing (`conn.go:584`).
When the peer stops reading, the pipe fills, the write loop blocks, and the 100-slot queue fills up.
From then on, every `SessionUpdate` and `StartRequest` blocks until the whole connection closes, even
after its turn has been cancelled.

**Proposal.**
- Change the signature to `send(ctx, msg)` and add `case <-ctx.Done()` to the wait.
- Responses keep using the connection context.
- Add a test that fills a small queue (`WithWriteQueueSize(1)`) against a transport that blocks.

### C3. Stop reporting unhandled notifications as errors
*Source: go-acp-sdk + review. Size: S (generator change).*

**Problem.** When no handler exists for a notification, the generated dispatch returns
`MethodNotFound` (`acp1/methods.gen.go:785`, `acp2/methods.gen.go:618`). `handleNotification` then
passes that to `WithErrorHandler` (`conn.go:448`). The examples send that handler to
`logger.Error`, so one unhandled notification kind turns into an error log line every time it
arrives; `document/didChange` would fire on every edit. The protocol says to ignore notifications
you don't recognize. go-acp-sdk drops `_` notifications silently.

**Proposal.**
- In `emitter.dispatch` (`facade/spec.go`), emit `return nil` for notifications that nothing
  handles, then regenerate.
- Optionally, only send `_`-prefixed unknown requests to `ExtMethodHandler`, and answer other
  unknown methods with `-32601` directly.

### C4. Timestamps that sort correctly, plus a session-info helper
*Source: review (C4a); go-acp-sdk `SendSessionInfo` and `TitleFromPrompt` (C4b). Size: S.*

**C4a. Problem.** `session/list` pages sort `updatedAt` by comparing strings (`session_store.go:82-85`).
The open-agent example writes it with `time.RFC3339Nano` (`examples/open-agent/sessions.go:64`),
which drops trailing zeros. As a result `10:00:00.5Z` sorts **older** than `10:00:00Z`, because
`Z` > `.` (confirmed). Pages then come out in the wrong order, and cursors can skip or repeat
sessions.

**C4a. Proposal.**
- Add `acp.FormatTimestamp(t time.Time) string`, which formats `t` in UTC with a fixed width
  (`2006-01-02T15:04:05.000000000Z`).
- Point to it from the `SessionInfoReporter` and `SessionListPosition` docs.
- Use it in the examples.

**C4b.** Add `SessionStream.SendSessionInfo(ctx, title string)` to both façades. It sets `updatedAt`
to `FormatTimestamp(time.Now())`. Also add `TitleOf(prompt []ContentBlock, maxRunes int) string`.
go-acp-sdk's version cuts by bytes and can split a UTF-8 character; this one cuts by runes.

### C5. Recover from panics in v2 `StartTurn` work
*Source: review. Size: S.*

**Problem.** `acp2.SessionManager.StartTurn` runs `work` in a plain goroutine
(`acp2/session_store.go:181`). The connection's `recover` (`conn.go:402`) only protects handlers, so
a panic there crashes the whole agent process. That ends every session, and on an `acphttp` server
every connection. v1 `RunTurn` doesn't have this problem because it runs inside the handler.

**Proposal.**
- `defer`-recover in that goroutine and always call `done()` and `Idle`, so client `Turn`s end.
- Report the panic through a new `WithTurnPanicHandler(func(any))` `SessionManagerOption`. By
  default, log it with `slog`.

### C6. Honour `line` and `limit` for `fs/read_text_file`
*Source: review. Size: S.*

**Problem.** The example client's `ReadTextFile` ignores `Line` and `Limit`
(`examples/client/main.go:76`) and returns the whole file. Code copied from it gets this wrong.

**Proposal.**
- Add `LineRange(content string, line, limit *uint32) string` to both façades. `line` is 1-based.
- Use it in the example.
- Let `SessionStream.ReadTextFile` take line and limit options as well.

### C7. Add CI
*Source: go-acp-sdk. Size: S.*

**Problem.** Neither upstream nor this fork has `.github/`. `-check`, the fuzz seeds and the Zod
snapshot only run when someone remembers to run them. go-acp-sdk runs build, vet, gofmt and
`test -race` on every push.

**Proposal.** A workflow on Go 1.27 that runs:
- `gofmt -l`, `go vet ./...` and `go test -race ./...`
- `(cd internal/cmd/schema && go test ./... && go run . -source ../../../schema/typescript -out ../../../schema -facade ../../.. -check)`.
  This needs CGO and a C compiler, which ubuntu runners already have.
- `(cd acpmcp && go test ./...)`

---

## Quality of life

### Q1. More content constructors
*Source: go-acp-sdk `helpers.go`. Size: S.*

Today only `TextBlock` and `ToolText` exist. Add these to both `content.go` files:
- `ImageBlock(data []byte, mimeType string)`, which base64-encodes for you. The schema field is a
  base64 string and that step is easy to forget.
- `AudioBlock(data []byte, mimeType string)`
- `ResourceLinkBlock(name, uri string)`
- `TextResourceBlock(uri, text string)`
- `BlobResourceBlock(uri string, data []byte, mimeType string)`
- `ToolContent(block ContentBlock)`

### Q2. Session-scoped calls on `ClientSession`
*Source: review. Size: S.*

`ClientSession` only has `Prompt` and `Cancel` (`acp1/session.go:51,71`). Callers repeat the session
id for everything else; the example client calls `agent.SetSessionMode(ctx, &{SessionID: session.ID, …})`.

Add, where each version has them:
- `SetMode(ctx, id)`
- `SetConfigOption(ctx, id, value)`
- `Close(ctx)`
- `Delete(ctx)`

### Q3. Return the replayed history from load and resume
*Source: review. Size: M.*

**Problem.** Examples track replay by hand with a `replaying atomic.Bool` flag
(`examples/dual-client/v2.go:15-58`, `examples/http-client`).

**Proposal.** Add `ClientSideConnection.LoadSession`-style helpers that return
`(*ClientSession, *Turn, response, error)`. The `Turn` yields the updates replayed during the call.
- The existing internals already support this: `Turns.Begin` before sending, and `End` when the
  response arrives.
- Notifications and responses are both handled in order on the read loop, so every replayed update
  lands before the call returns.
- v2 `ResumeSession` with `ReplayFrom` is the same shape.

### Q4. A tool-call handle
*Source: review. Size: S–M.*

**Problem.** Every tool helper in the examples repeats the same steps: generate an id,
`StartToolCall`, then `CompleteToolCall` or `FailToolCall`. Asking permission means rebuilding a
`ToolCallUpdate` with the title, kind and locations that were already sent (`examples/agent/tools.go:133-150`).

**Proposal.** `stream.ToolCall(ctx, title, kind, opts...) (*ToolCall, error)`:
- It generates the id and remembers the title, kind and locations.
- It has `Update`, `Complete`, `Fail` and `RequestPermission(ctx, options...)`. `RequestPermission`
  fills in the `ToolCallUpdate` itself. In v2 it maps onto the title/subject shape.

### Q5. Remember the peer's capabilities from `initialize`
*Source: review. go-acp-sdk bans capability checks, but acp-go already relies on them through
`CapabilitiesOf`. Size: S–M.*

**Problem.**
- Agents store client capabilities by hand (`examples/agent/main.go:61`).
- `SessionStream.ReadTextFile` checks `s.client.(FileReader)` (`acp1/session_stream.go:253-256`).
  A real `AgentSideConnection` always passes that check, so a client without `fs.readTextFile` gets
  the request anyway and answers `-32601`.

**Proposal.**
- Record `ClientCapabilities` and `ClientInfo` on `AgentSideConnection`. Add `Via: "initialize"` to
  the agent-side `initialize` entry in the façade tables.
- Record the `InitializeResponse` on `ClientSideConnection`, inside the existing `initialize`
  `CallVia`.
- Expose them as `conn.ClientCapabilities()` and `agent.AgentCapabilities()`.
- `SessionStream`'s file and terminal helpers then fail early with a sentinel `acp.ErrNotAdvertised`
  that `errors.Is` can match.

### Q6. Bearer-token helpers for `acphttp`
*Source: go-acp-sdk `ws.WithBearerToken` and `BearerTokenAuth`. Size: S.*

- Client: `acphttp.WithBearerToken(token)`, a shorthand for `WithHeader("Authorization", …)`.
- Server: `acphttp.RequireBearerToken(token, next http.Handler)`, comparing with
  `subtle.ConstantTimeCompare`. go-acp-sdk compares with `==`, which leaks timing.

---

## Expanded capabilities

### E1. Remember permission decisions, with optional rules
*Source: go-acp-sdk `agentutil/permissions.go`. Size: M.*

**Problem.** `DefaultPermissionOptions` offers "Always allow" (`acp1/permission.go:10`), but nothing
remembers that answer, so the next call asks again. The open-agent example avoids the problem by
never offering "always" (`examples/open-agent/tools.go:370-372`).

**Proposal.** A `PermissionPolicy` that can be saved as JSON, so it can live in `FileStore` session
state:
- It records `allow_always` and `reject_always` answers under a key the caller picks, such as
  `"edit:" + path`, and answers repeats without asking the client again.
- It can hold optional allow/deny/ask rules. Match them with `path.Match` globs; go-acp-sdk only
  supports prefix and suffix matching.
- Guard it with a mutex. go-acp-sdk's version has none.
- The generic core goes in the root package, with a thin wrapper per façade:
  `policy.Request(ctx, stream, key, toolCall, options...)`.

### E2. Bridge transports, and a proxy command
*Source: go-acp-sdk `ws/proxy.go` and `cmd/proxy`. Size: M.*

**Proposal.** `acp.Bridge(ctx, a, b acp.Transport) error` copies messages in both directions until
either side ends. It should propagate `ctx` and close both ends properly; go-acp-sdk's proxy ignores
`ctx` and write errors. On top of it:
- `cmd/acp-proxy -url https://host/acp [-H …]` lets an editor that can only spawn stdio agents
  reach a remote agent over Streamable HTTP or WebSocket. The acphttp client transports already
  inspect the messages that pass through, so a raw copy is enough.
- `acp.BridgeCommand(ctx, cmd, t)` serves an existing stdio agent binary through
  `acphttp.NewServer`, reusing `acpconn.Spawn`'s process-group and `ExitGrace` handling.

### E3. Fan out session updates to several connections
*Source: go-acp-sdk `SessionBroadcaster`. Size: M. Worth doing once v2's multi-client story settles.*

**Proposal.** A broadcaster that implements `Client`:
- It sends `SessionUpdate` to every connection subscribed to a session.
- It sends requests (`RequestPermission` and the rest) to one primary connection. go-acp-sdk
  answers them with `-32601` instead.
- It unsubscribes a connection automatically when its `Done()` closes.
- It has `SendExcept` for echoing a user's message to the other watchers.

Fix go-acp-sdk's problems while porting it:
- one goroutine leaked per `Subscribe` call
- no check for duplicate subscriptions

### E4. An optional history log that serves `session/load`
*Source: go-acp-sdk `storage` event tree. Size: L.*

**Problem.** `SessionManager` leaves `session/load` to the agent because only the agent knows the
conversation. Most agents would be satisfied with "replay the updates I sent".

**Proposal.**
- An `acp.HistoryStore[ID]` interface with `Append(ctx, id, update)` and `Load(ctx, id)`, which
  returns an iterator.
- Two implementations: in-memory, and a JSONL file per session.
- Name the files by hex-encoding the session id, as `FileStore` does. go-acp-sdk's `DirStore` uses
  ids as path parts and can be tricked into writing outside its directory.
- A `SessionStream` option that records each update it sends.
- A `SessionManager` option that implements `LoadSession` by replaying the log.
- go-acp-sdk also has fork and revert through head pointers. Leave those out until someone needs
  them.

---

## Not worth porting

- **Hand-written types with no code generation.** acp-go's generator already gives it Zod
  validation, unions that tolerate unknown tags, and a drift check. It is ahead here.
- **Strict unions that fail on unknown tags.** One unknown `sessionUpdate` kind fails the whole
  notification.
- **Typed `func(Agent) Agent` middleware.** Wrapping the agent hides its optional interfaces from
  dispatch, which finds them by type assertion. That is the root cause of go-acp-sdk's
  `agentsmd` bug. If this is wanted, generate forwarding wrappers from the façade table instead.
- **The `agentsmd` middleware as written.** It has a nil embedded client, reads files without a
  `sessionId`, and adds duplicate blocks.
- **go-acp-sdk's `$/cancel_request` handling.** It cancels its own outgoing call rather than the
  peer's request. acp-go handles this correctly.

## Status

C1–C7 are implemented, each on its own branch off upstream `74e8b95` with its own tests. Each
branch is one commit and can become a separate PR. All seven merge cleanly together, and the
combined tree passes every suite.

| Proposal | Branch |
| --- | --- |
| C1 | `claude/c1-answer-undecodable-messages` |
| C2 | `claude/c2-send-respects-caller-context` |
| C3 | `claude/c3-ignore-unhandled-notifications` |
| C4 | `claude/c4-sortable-timestamps` |
| C5 | `claude/c5-recover-turn-panics` |
| C6 | `claude/c6-read-text-file-lines` |
| C7 | `claude/c7-ci` |

Two changes from the proposals above:
- C1 replies only when the lenient re-read finds an id. Garbage with no id has nobody waiting on
  it, so it is still only logged; it does not get a `null`-id reply. A response that fails to
  decode now fails the call waiting on it.
- C3 changes only notifications. Unknown requests without a `_` prefix still go to
  `ExtMethodHandler`, because an existing test relies on that.

## Suggested order

1. **Small correctness fixes:** C1, C2, C3, C4, C5, C6 and C7.
2. **Helpers:** Q1, Q2, Q6, then Q5 and Q4.
3. **Larger additions:** Q3 and E1, then E2, then E3 and E4.
