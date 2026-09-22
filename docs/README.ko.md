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
루트 `acp` 패키지는 `schema/v1` 위에서 ACP v1을 구현하며, `schema/v2` 용 파사드는 아직 없습니다.

## 설치

```bash
go get github.com/ironpark/go-acp
```

## 기능

- **JSON-RPC 2.0** 양방향 통신 (`encoding/json/v2`)
- **스키마 검증** - 들어오는 파라미터를 SDK의 Zod 규칙으로 검증한 뒤 핸들러에 전달
- **요청 단위 취소** - 양방향 `$/cancel_request`, `-32800` 응답
- **플러그형 Transport** - stdio, HTTP+SSE
- **미들웨어** - 요청/알림 양쪽을 감싸는 조합 가능한 체인
- **SessionManager** - 세션 생성/로드/목록/삭제 메서드 제공
- **SessionStream** - 세션 업데이트 전송 편의 API
- **선택적 인터페이스** - 구현하지 않은 메서드는 자동으로 `-32601` 응답

## 빠른 시작

### 에이전트 구현

```go
conn := acp.NewAgentSideConnection(func(c *acp.AgentSideConnection) acp.Agent {
    return &MyAgent{client: c} // 연결 자체가 상대편 Client 입니다
}, os.Stdin, os.Stdout)

if err := conn.Start(context.Background()); err != nil {
    log.Fatal(err)
}
```

`Agent` 인터페이스에 반드시 필요한 메서드는 `Initialize`, `Authenticate`, `NewSession`,
`Prompt`, `Cancel` 다섯 개뿐입니다. 나머지는 선택적 인터페이스(`acp.SessionLoader`,
`acp.SessionLister`, `acp.SessionModeSetter`, `acp.NesHandler` 등)로 구현하고
`Initialize` 응답에서 해당 capability를 알리면 됩니다.

### 클라이언트 구현

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

`Client` 인터페이스에 필요한 메서드는 `SessionUpdate`와 `RequestPermission` 두 개입니다.
파일 시스템·터미널·elicitation 지원은 `acp.FileReader`, `acp.FileWriter`,
`acp.TerminalHandler`, `acp.ElicitationHandler`로 추가합니다.

## 아키텍처

- **`AgentSideConnection`**: `Agent`를 제공하고 상대편 클라이언트를 호출
- **`ClientSideConnection`**: `Client`를 제공하고 상대편 에이전트를 호출
- **`Transport`**: 플러그형 프레이밍 (기본 stdio, HTTP+SSE 포함)
- **`SessionManager`**: `SessionStore` 기반 세션 수명주기 메서드
- **`SessionStream`**: union을 직접 만들지 않고 세션 업데이트 전송
- **`Middleware`**: 요청/알림을 감싸는 래퍼
- **`TerminalHandle`**: 터미널 ID와 세션 ID를 묶은 핸들
- **`schema/v1`**: 생성된 와이어 타입, union, Zod 검증

## 주요 기능

### 취소

나가는 호출의 컨텍스트를 취소하면 해당 요청 ID로 `$/cancel_request`를 보냅니다.
받는 쪽에서는 해당 핸들러의 컨텍스트가 취소되고, 핸들러가 먼저 응답하지 않으면
`-32800 Request cancelled`가 전달됩니다. 프롬프트 턴 전체를 취소하는
`session/cancel`과는 별개입니다.

### 세션 관리

```go
manager := acp.NewSessionManager(
    acp.NewMemoryStore[*MySession](),
    func(ctx context.Context, params *acp.NewSessionRequest) (acp.SessionID, *MySession, error) {
        return acp.GenerateSessionID(), &MySession{cwd: params.Cwd}, nil
    },
)

type MyAgent struct {
    *acp.SessionManager[*MySession] // NewSession, LoadSession, ListSessions, DeleteSession 제공
}
```

에이전트에 같은 이름의 메서드를 직접 선언하면 그 메서드가 우선합니다.

### 미들웨어

```go
conn := acp.NewAgentSideConnection(newAgent, os.Stdin, os.Stdout,
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
case schema.SessionUpdateAgentMessageChunk:
    if text, ok := update.Content.Variant().(schema.ContentBlockText); ok {
        fmt.Print(text.Text)
    }
case schema.SessionUpdateToolCall:
    fmt.Println(update.Title)
}

update := schema.NewSessionUpdate(schema.SessionUpdatePlan{Entries: entries})
```

### 연결 옵션

```go
acp.NewAgentSideConnection(newAgent, os.Stdin, os.Stdout,
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
| `initialize`, `authenticate`, `session/new`, `session/prompt`, `session/cancel` | `Agent` (필수) |
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

`mcp/*` 메서드는 참조 SDK와 마찬가지로 전용 핸들러가 없으며 `ExtMethodHandler`로 전달됩니다.
`$/cancel_request`는 연결이 직접 처리합니다.

## 예제

완전한 작동 예제는 [docs/example](./example/) 디렉토리를 참조하세요:

- **[에이전트 예제](./example/agent/)** - 세션, 스트리밍 업데이트, 권한 요청
- **[클라이언트 예제](./example/client/)** - 에이전트 프로세스 실행과 프롬프트 턴 진행

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
