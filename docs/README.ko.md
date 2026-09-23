![agent client protocol golang banner](./imgs/banner-dark.jpg)

# Agent Client Protocol - Go 구현체

Agent Client Protocol (ACP)의 Go 구현체입니다. ACP는 _코드 에디터_(소스 코드를 보고 편집하는 대화형 프로그램)와 _코딩 에이전트_(생성형 AI를 사용하여 자율적으로 코드를 수정하는 프로그램) 간의 통신을 표준화합니다.

이것은 Go로 작성된 ACP 사양의 **비공식** 구현체입니다. 공식 프로토콜 사양과 참조 구현체는 [공식 저장소](https://github.com/zed-industries/agent-client-protocol)에서 찾을 수 있습니다.

> [!NOTE]
> Agent Client Protocol은 활발히 개발 중입니다. 이 구현체는 최신 사양 변경사항을 따라가지 못할 수 있습니다. 가장 최신의 프로토콜 사양은 [공식 저장소](https://github.com/zed-industries/agent-client-protocol)를 참조해 주세요.

프로토콜에 대한 자세한 내용은 [agentclientprotocol.com](https://agentclientprotocol.com/)에서 확인하세요.

## `next` 브랜치

이 브랜치는 Go 1.27+ 를 요구하며 SDK를 `encoding/json/v2` 기반으로 다시 만든 버전입니다.
와이어 타입은 공식 TypeScript SDK에서 `go-tree-sitter`로 생성합니다
([스키마 생성](../schema/README.md) 참고).
루트 `acp` 패키지는 모든 프로토콜 버전이 공유하는 것 — 옵션, transport, 미들웨어, 에러 타입,
세션 스토어 — 만 담습니다. 프로토콜 파사드는 버전별 형제 패키지입니다: `schema/v1` 위의
[`acpv1`](../acpv1/)(안정)과 `schema/v2` 위의 [`acpv2`](../acpv2/)(초안, 바뀔 수 있음).
[`router`](../router/)가 한 엔드포인트에서 두 버전을 함께 서비스합니다.

## 설치

```bash
go get github.com/ironpark/go-acp
```

## 기능

- **JSON-RPC 2.0** 양방향 통신 (`encoding/json/v2`)
- **스키마 검증** - 들어오는 파라미터를 SDK의 Zod 규칙으로 검증한 뒤 핸들러에 전달
- **요청 단위 취소** - 양방향 `$/cancel_request`, `-32800` 응답
- **플러그형 Transport** - stdio, Streamable HTTP, WebSocket
- **미들웨어** - 요청/알림 양쪽을 감싸는 조합 가능한 체인
- **SessionManager** - 세션 생성/로드/목록/삭제 메서드와 턴 취소 제공
- **SessionStream** - 세션 업데이트 전송 편의 API
- **ClientSession / Turn** - 클라이언트에서 세션에 프롬프트를 보내고 그 턴의 업데이트를 읽는 API
- **타입 있는 확장 메서드** - `acp.CallExt`, `acp.ExtRouter`, 그리고 `_meta`용 `acpv1.Meta`
- **모르는 union variant 보존** - 새 버전 피어의 variant도 디코드 실패 없이 `…Unknown`으로 보존
- **선택적 인터페이스** - 구현하지 않은 메서드는 자동으로 `-32601` 응답

## 빠른 시작

### 에이전트 구현

```go
conn := acpv1.NewAgentSideConnection(func(c *acpv1.AgentSideConnection) acpv1.Agent {
    return &MyAgent{client: c} // 연결 자체가 상대편 Client 입니다
}, os.Stdin, os.Stdout)

if err := conn.Start(context.Background()); err != nil {
    log.Fatal(err)
}
```

`Agent` 인터페이스에 반드시 필요한 메서드는 `Initialize`, `NewSession`, `Prompt`, `Cancel`
네 개뿐입니다. 나머지는 선택적 인터페이스(`acpv1.Authenticator`, `acpv1.SessionLoader`,
`acpv1.SessionLister`, `acpv1.SessionModeSetter`, `acpv1.NesHandler` 등)로 구현합니다.
`acpv1.CapabilitiesOf(agent)`가 구현된 인터페이스에서 capability를 유도해 주므로, `Initialize`
응답이 연결이 거부할 메서드를 광고하는 일이 없습니다:

```go
caps := acpv1.CapabilitiesOf(a)
caps.PromptCapabilities = &schema.PromptCapabilities{Image: new(true)} // 콘텐츠 capability는 직접 설정
return &acpv1.InitializeResponse{ProtocolVersion: acpv1.ProtocolVersion, AgentCapabilities: caps}, nil
```

### 클라이언트 구현

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

turn, _ := session.Prompt(ctx, acpv1.TextBlock("README.md를 요약해줘"))
for update := range turn.Updates() {
    render(update) // 도구 호출, 계획, 메시지 조각...
}
response, err := turn.Wait() // 또는: text, response, err := turn.Text()
```

`SpawnAgent`는 읽기 루프를 이미 시작한 상태로 돌려주며, `agent.Wait()`가 프로세스와 연결이 어떻게
끝났는지 알려 줍니다. `cmd.Stderr`를 지정하지 않으면 에이전트의 stderr는 부모 프로세스로 전달됩니다.
`acpv1.Pipe`는 에이전트와 클라이언트를 메모리에서 연결하므로 테스트에 유용합니다.

`Client` 인터페이스에 필요한 메서드는 `SessionUpdate`와 `RequestPermission` 두 개입니다.
`SessionUpdate`는 `Turn` 밖의 업데이트까지 모두 받습니다. 알림은 읽기 루프에서 순서대로 처리되므로,
핸들러 안에서 에이전트 호출의 응답을 기다리면 교착 상태가 됩니다.
파일 시스템·터미널·elicitation 지원은 `acpv1.FileReader`, `acpv1.FileWriter`,
`acpv1.TerminalHandler`, `acpv1.ElicitationHandler`로 추가하며, `acpv1.ClientCapabilitiesOf(client)`가
대응하는 플래그를 유도합니다.

## 아키텍처

- **`acp`** (루트): `Option`, `Transport`(stdio, Streamable HTTP, WebSocket), `Middleware`, `RequestError`, `SessionStore`,
  `TurnTracker`, 타입 있는 확장(`CallExt`, `ExtRouter`)
- **`acpv1.AgentSideConnection`**: `Agent`를 제공하고 상대편 클라이언트를 호출
- **`acpv1.ClientSideConnection`**: `Client`를 제공하고 상대편 에이전트를 호출
- **`acpv1.SpawnAgent`**, **`acpv1.Pipe`**: 자식 프로세스 에이전트, 또는 메모리 내 양쪽 연결
- **`acpv1.ClientSession`**, **`acpv1.Turn`**: 세션에 프롬프트를 보내고 그 턴의 업데이트를 읽음
- **`acpv1.SessionManager`**: 스토어 기반 세션 수명주기와 턴 취소
- **`acpv1.SessionStream`**: union을 직접 만들지 않고 세션 업데이트 전송
- **`acpv1.TerminalHandle`**: 터미널 ID와 세션 ID를 묶은 핸들
- **`acpv1.CapabilitiesOf`**: 에이전트가 구현한 인터페이스에서 capability 유도
- **`acpv2`**: 초안 ACP v2(`schema/v2`)용 동일 구조의 파사드
- **`router.ProtocolRouter`**: v1·v2 에이전트를 한 엔드포인트로 서비스, **`router.ClientConnector`**: 클라이언트 쪽 v2 우선 연결과 v1 fallback
- **`schema/v1`, `schema/v2`**: 생성된 와이어 타입, union, Zod 검증

## 주요 기능

### 취소

나가는 호출의 컨텍스트를 취소하면 해당 요청 ID로 `$/cancel_request`를 보냅니다.
받는 쪽에서는 해당 핸들러의 컨텍스트가 취소되고, 핸들러가 먼저 응답하지 않으면
`-32800 Request cancelled`가 전달됩니다. 프롬프트 턴 전체를 취소하는
`session/cancel`과는 별개입니다([세션 관리](#세션-관리) 참고).

### 에러

핸들러에서 `acp.Err…` 생성자를 반환하면 상대에게 갈 코드를 고를 수 있고, 그 외 에러는 `-32603`이 됩니다.
상대가 보낸 에러는 `*acp.RequestError`로 돌아옵니다:

```go
if acp.IsCode(err, acp.ErrorCodeAuthRequired) {
    // 인증 후 재시도
}
```

### 확장 메서드와 `_meta`

```go
type MyAgent struct {
    acp.ExtRouter // ExtMethodHandler와 ExtNotificationHandler를 구현
    // ...
}

a.HandleExt("_example.com/index", func(ctx context.Context, p *IndexParams) (*IndexResult, error) {
    return &IndexResult{Files: 42}, nil
})

result, err := acp.CallExt[IndexResult](ctx, conn, "_example.com/index", IndexParams{Path: "."})
```

파라미터 디코드에 실패하면 `-32602`, 등록되지 않은 메서드는 `-32601`로 응답하고, 등록되지 않은 알림은
무시합니다. 모든 `_meta` 필드는 값을 원본 JSON 그대로 보관하는 `acpv1.Meta` 타입입니다:

```go
var meta acpv1.Meta
meta.Set("trace", Trace{ID: "abc"})
trace, ok, err := params.Meta.Get[Trace]("trace")
```

### v1과 v2 동시 지원

```go
r := router.New().
    WithV1(func(c *acpv1.AgentSideConnection) acpv1.Agent { return &v1Agent{client: c} }).
    WithV2(func(c *acpv2.AgentSideConnection) acpv2.Agent { return &v2Agent{client: c} })
err := r.ServeStdio(ctx, os.Stdin, os.Stdout)
```

라우터는 첫 메시지(`initialize`여야 함)를 읽어 요청 버전 이하 중 가장 높은 설정 버전을 고르고,
initialize 파라미터만 그 버전 모양으로 고칩니다(v1 전용 에이전트에 v2 요청이 오면 `info` → `clientInfo`,
`fs`/`terminal` 없음으로 다운그레이드). 이후 메시지는 그대로 전달합니다.
옵션·transport·미들웨어는 루트 `acp` 패키지에 있어 한 값으로 양쪽 파사드를 설정합니다.

두 버전을 모두 지원하는 클라이언트는 `router.NewClient`를 씁니다. 에이전트를 띄워 v2로 initialize하고,
에이전트가 `protocolVersion` 1로 응답하거나 v2 요청을 거절하면 v1으로 다시 띄웁니다. 그래서 각 버전은
자기 모양의 initialize 요청을 보냅니다:

```go
agent, err := router.NewClient().
    WithV1(newV1Client, &acpv1.InitializeRequest{ClientCapabilities: v1Caps}).
    WithV2(newV2Client, &acpv2.InitializeRequest{Info: info}).
    Spawn(ctx, func() *exec.Cmd { return exec.Command("my-agent") })
defer agent.Close() // Close, Wait, Done, 확장 호출은 버전과 상관없이 동작
if agent.V2 != nil {
    // agent.V2, agent.V2Init
} else {
    // agent.V1, agent.V1Init
}
```

원격 에이전트에는 `Connect`에 dial 함수를 넘깁니다. 시도할 때마다 한 번씩 호출됩니다:

```go
agent, err := router.NewClient().WithV1(…).WithV2(…).
    Connect(ctx, func(ctx context.Context) (acp.Transport, error) {
        return acp.NewHTTPClientTransport("https://host/acp"), nil // 또는 acp.DialWebSocket
    })
```

재연결은 다른 SDK와 같이 새 연결입니다: 같은 헤더와 `acp.WithCookieJar(jar)`로 다시 연결해 로드 밸런서의
affinity 쿠키가 같은 백엔드로 보내게 하고, `Initialize` 뒤 에이전트가 `loadSession`을 지원하면 저장해 둔
세션 id로 `LoadSession`합니다. 끊겨 있던 동안의 메시지는 재전송되지 않습니다(프로토콜 v2의 몫).

### 세션 관리

```go
manager := acpv1.NewSessionManager(
    acpv1.NewMemoryStore[*MySession](),
    func(ctx context.Context, params *acpv1.NewSessionRequest) (acpv1.SessionID, *MySession, error) {
        return acpv1.GenerateSessionID(), &MySession{cwd: params.Cwd}, nil
    },
)

type MyAgent struct {
    *acpv1.SessionManager[*MySession] // NewSession, Cancel, LoadSession, ListSessions, DeleteSession 제공
}

func (a *MyAgent) Prompt(ctx context.Context, params *acpv1.PromptRequest) (*acpv1.PromptResponse, error) {
    ctx, done, err := a.BeginTurn(ctx, params.SessionID) // 매니저의 Cancel이 ctx를 취소
    if err != nil {
        return nil, err // acp.ErrTurnInProgress: v1 세션은 한 번에 한 턴
    }
    defer done()
    if err := a.work(ctx); context.Cause(ctx) == acp.ErrTurnCancelled {
        return &acpv1.PromptResponse{StopReason: acpv1.StopReasonCancelled}, nil
    } else if err != nil {
        return nil, err
    }
    return &acpv1.PromptResponse{StopReason: acpv1.StopReasonEndTurn}, nil
}
```

에이전트에 같은 이름의 메서드를 직접 선언하면 그 메서드가 우선합니다.

### SessionStream

```go
stream := acpv1.NewSessionStream(client, sessionID)

stream.SendText(ctx, "안녕하세요!")
stream.StartToolCall(ctx, toolID, "파일 읽기", acpv1.ToolKindRead)
stream.CompleteToolCall(ctx, toolID, acpv1.ToolText(contents))
stream.Send(ctx, acpv1.SessionUpdateSessionInfoUpdate{Title: new("리팩터링")}) // 헬퍼가 없는 variant용
stream.WithMeta(meta).SendText(ctx, "…")                                    // 모든 알림에 _meta 첨부
```

흔한 텍스트 콘텐츠는 `acpv1.TextBlock`, `acpv1.TextOf`, `acpv1.Texts`(프롬프트의 텍스트 블록 iterator),
`acpv1.ToolText`로 다룹니다. v2 `SessionStream`은
메시지마다 id를 받고, 명시적인 턴 상태를 위한 `Running`, `RequiresAction`, `Idle`을 제공합니다.

### 미들웨어

```go
conn := acpv1.NewAgentSideConnection(newAgent, os.Stdin, os.Stdout,
    acp.WithMiddleware(
        acp.LoggingMiddleware(logger.Printf),
        acp.TimeoutMiddleware(30*time.Second),
    ),
)
```

핸들러의 패닉은 연결이 직접 복구해 `-32603`으로 응답하므로 별도의 recovery 미들웨어가 필요하지 않습니다.

### Union 처리

생성된 union은 sealed variant 인터페이스를 감싸므로, 기존 matcher 대신 타입 스위치를 사용합니다:

```go
switch update := notification.Update.Variant().(type) {
case acpv1.SessionUpdateAgentMessageChunk:
    if text, ok := acpv1.TextOf(update.Content); ok {
        fmt.Print(text)
    }
case acpv1.SessionUpdateToolCall:
    fmt.Println(update.Title)
}

update := acpv1.NewSessionUpdate(acpv1.SessionUpdatePlan{Entries: entries})
```

이 SDK가 모르는 태그가 와도 메시지가 실패하지 않습니다. 스키마가 정의한 `Custom` variant가 있으면 그쪽으로,
없으면 생성된 `…Unknown` variant(예: `acpv1.SessionUpdateUnknown`)로 디코드되며, `Raw` 필드에 받은 객체가
그대로 담겨 다시 인코딩할 때도 바뀌지 않습니다. `default` 케이스에서 처리하거나, 프로토콜 권고대로 무시하면 됩니다.

### 연결 옵션

```go
acpv1.NewAgentSideConnection(newAgent, os.Stdin, os.Stdout,
    acp.WithWriteQueueSize(500),               // 쓰기 큐 크기
    acp.WithRequestTimeout(30*time.Second),    // 나가는 호출 기본 타임아웃
    acp.WithShutdownTimeout(10*time.Second),   // 셧다운 대기 한도
    acp.WithErrorHandler(func(err error) {}),  // 치명적이지 않은 에러 콜백
)
```

## 프로토콜 지원

ACP 프로토콜 버전 1 ([`schema/typescript/REVISION`](../schema/typescript/REVISION) 기준).

### 에이전트 메서드 (클라이언트 → 에이전트)

| 메서드 | Go 인터페이스 |
| --- | --- |
| `initialize`, `session/new`, `session/prompt`, `session/cancel` | `Agent` (필수) |
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

### 클라이언트 메서드 (에이전트 → 클라이언트)

| 메서드 | Go 인터페이스 |
| --- | --- |
| `session/update`, `session/request_permission` | `Client` (필수) |
| `fs/read_text_file` | `FileReader` |
| `fs/write_text_file` | `FileWriter` |
| `terminal/*` | `TerminalHandler` |
| `elicitation/create`, `elicitation/complete` | `ElicitationHandler` |

v1의 `mcp/*` 메서드는 참조 SDK와 마찬가지로 전용 핸들러가 없으며 `ExtMethodHandler`로 전달됩니다.
`$/cancel_request`는 연결이 직접 처리합니다.

### ACP v2 (`acpv2`, 초안)

| 메서드 | Go 인터페이스 |
| --- | --- |
| `initialize`, `session/new`, `session/prompt`, `session/cancel` | `Agent` (필수) |
| `auth/login`, `auth/logout` | `AuthHandler` |
| `session/list`, `session/delete`, `session/fork`, `session/resume`, `session/close` | `SessionLister`, `SessionDeleter`, `SessionForker`, `SessionResumer`, `SessionCloser` |
| `session/set_config_option` | `SessionConfigOptionSetter` |
| `providers/*` | `ProviderManager` (unstable) |
| `nes/*`, `document/did*` | `NesHandler`, `DocumentHandler` (unstable) |
| `mcp/message` (에이전트 측) | `MCPMessageHandler` (unstable) |
| `session/update`, `session/request_permission` | `Client` (필수) |
| `mcp/connect`, `mcp/message`, `mcp/disconnect` | `MCPConnector` (unstable) |
| `elicitation/create`, `elicitation/complete` | `ElicitationHandler` |

v2에는 `fs/*`·`terminal/*` 메서드가 없으므로(파일·셸 접근은 MCP 경유) `TerminalHandle`은 v1 패키지에만
있습니다. v2에서 prompt 응답은 메시지 접수만 뜻하며, `Turn`은 에이전트가 idle 상태를 보고할 때 끝납니다.

턴이 겹치는 프롬프트는 버전별 규칙을 양쪽이 똑같이 따릅니다. v1 세션은 한 번에 한 턴만 돌리므로
`ClientSession.Prompt`와 `SessionManager.BeginTurn` 모두 두 번째 프롬프트를 `acp.ErrTurnInProgress`(`-32600`)로
거절합니다. v2에서는 프롬프트가 진행 중인 작업에 합류할 수 있습니다. `Prompt`는 메시지가 접수되면
`(*Turn, MessageID, error)`를 돌려주며 진행 중인 턴에 합류하고, `SessionManager.JoinTurn`은 에이전트에게 진행 중인
턴의 context를 넘겨줍니다.

## 예제

완전한 작동 예제는 [docs/example](./example/) 디렉토리를 참조하세요:

- **[에코 에이전트](./example/echo/)** - 가장 작은 에이전트: 필수 메서드 네 개
- **[에이전트](./example/agent/)** - 세션, 취소, 도구 호출, 권한 요청, 확장 메서드
- **[클라이언트](./example/client/)** - 모든 stdio 에이전트용 대화형 클라이언트, Ctrl-C로 턴 취소
- **[HTTP 에이전트](./example/http-agent/) / [HTTP 클라이언트](./example/http-client/)** - 같은 연결을 Streamable HTTP 또는 WebSocket으로
- **[듀얼 에이전트](./example/dual-agent/) / [듀얼 클라이언트](./example/dual-client/)** - 하나의 엔드포인트에서 v1과 v2 제공, v2에서 v1로 폴백하는 클라이언트

## 개발

### 빌드

```bash
go build ./...
```

### 테스트

```bash
go test ./...
```

## 기여하기

이것은 비공식 구현체입니다. 프로토콜 사양 변경사항은 [공식 저장소](https://github.com/zed-industries/agent-client-protocol)에 기여해 주세요.

Go 구현체 이슈 및 개선사항은 이슈를 열거나 풀 리퀘스트를 보내주세요.

## 라이선스

이 구현체는 공식 ACP 사양과 동일한 라이선스를 따릅니다.

## 관련 프로젝트

- **공식 ACP 저장소**: [zed-industries/agent-client-protocol](https://github.com/zed-industries/agent-client-protocol)
- **Rust 구현체**: 공식 저장소의 일부
- **프로토콜 문서**: [agentclientprotocol.com](https://agentclientprotocol.com/)

### ACP를 지원하는 에디터

- [Zed](https://zed.dev/docs/ai/external-agents)
- [neovim](https://neovim.io) - [CodeCompanion](https://github.com/olimorris/codecompanion.nvim) 플러그인을 통해
- [yetone/avante.nvim](https://github.com/yetone/avante.nvim): Cursor AI IDE의 동작을 에뮬레이트하도록 설계된 Neovim 플러그인
