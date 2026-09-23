// Command open-agent is an ACP coding agent backed by a model on OpenRouter.
//
// Where the agent example scripts its turns, this one lets a model drive them:
// it streams the model's answer and reasoning to the client and runs the tools
// the model calls (tools.go) through the client, which reads and writes files
// and runs commands in its terminal. In ask mode (modes.go) it asks before a
// write or a command. The OpenRouter client (openrouter.go) uses only the
// standard library.
//
// It reads its configuration from the environment:
//
//	OPENROUTER_API_KEY   required
//	OPENROUTER_MODEL     the model to use; default openai/gpt-oss-120b
//	OPENROUTER_BASE_URL  default https://openrouter.ai/api/v1; any
//	                     OpenAI-compatible endpoint works
package main

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
)

// session holds the state the agent keeps per ACP session.
type session struct {
	cwd string

	mu      sync.Mutex
	mode    acp1.SessionModeID
	history []message // the conversation so far, without the system prompt
	cost    float64   // the session's running cost in USD
}

// messages returns a copy of the conversation so far.
func (s *session) messages() []message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.history)
}

// commit appends messages to the conversation.
func (s *session) commit(messages ...message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, messages...)
}

// addCost adds the cost of a model call and returns the session's total.
func (s *session) addCost(cost float64) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cost += cost
	return s.cost
}

// openAgent embeds a SessionManager for the session lifecycle and turn
// cancellation. It needs no ACP authentication, so it leaves out
// Authenticate: the OpenRouter key comes from the environment.
type openAgent struct {
	*acp1.SessionManager[*session]
	client *acp1.AgentSideConnection
	llm    *openRouter

	// The tools offered to the model: only those the client can run.
	tools []tool
}

func (a *openAgent) Initialize(_ context.Context, params *acp1.InitializeRequest) (*acp1.InitializeResponse, error) {
	a.tools = offeredTools(params)
	return &acp1.InitializeResponse{
		ProtocolVersion:   acp1.ProtocolVersion,
		AgentCapabilities: acp1.CapabilitiesOf(a),
		AgentInfo:         &acp1.Implementation{Name: "open-agent", Version: "0.1.0"},
	}, nil
}

func (a *openAgent) Prompt(ctx context.Context, params *acp1.PromptRequest) (*acp1.PromptResponse, error) {
	sess, err := a.Lookup(ctx, params.SessionID)
	if err != nil {
		return nil, err
	}

	// The embedded manager's Cancel cancels this context, which also aborts
	// the request to the model.
	ctx, done, err := a.BeginTurn(ctx, params.SessionID)
	if err != nil {
		return nil, err
	}
	defer done()

	stopReason, err := a.runTurn(ctx, params.SessionID, sess, acp1.JoinTexts(params.Prompt))
	if err != nil {
		if context.Cause(ctx) == acp.ErrTurnCancelled {
			return &acp1.PromptResponse{StopReason: acp1.StopReasonCancelled}, nil
		}
		return nil, err
	}
	return &acp1.PromptResponse{StopReason: stopReason}, nil
}

// LoadSession replays the conversation to the client, which shows it as if
// it were happening now: the user's messages and the model's answers.
// Tool calls are not replayed.
func (a *openAgent) LoadSession(ctx context.Context, params *acp1.LoadSessionRequest) (*acp1.LoadSessionResponse, error) {
	sess, err := a.Lookup(ctx, params.SessionID)
	if err != nil {
		return nil, err
	}
	stream := acp1.NewSessionStream(a.client, params.SessionID)
	for _, m := range sess.messages() {
		switch {
		case m.Role == "user":
			err = stream.SendUserMessage(ctx, m.Content)
		case m.Role == "assistant" && m.Content != "":
			err = stream.SendText(ctx, m.Content)
		}
		if err != nil {
			return nil, err
		}
	}
	return &acp1.LoadSessionResponse{Modes: modeState(sess.currentMode())}, nil
}

func main() {
	// Stdout carries the protocol, so logs go to stderr.
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	llm := &openRouter{
		baseURL: cmp.Or(os.Getenv("OPENROUTER_BASE_URL"), "https://openrouter.ai/api/v1"),
		apiKey:  os.Getenv("OPENROUTER_API_KEY"),
		model:   cmp.Or(os.Getenv("OPENROUTER_MODEL"), "openai/gpt-oss-120b"),
	}
	if llm.apiKey == "" {
		logger.Error("set OPENROUTER_API_KEY to an OpenRouter API key: https://openrouter.ai/keys")
		os.Exit(1)
	}
	// The context length lets the agent report usage. An endpoint without
	// /models only loses that; a model the endpoint does not know is fatal.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := llm.lookupModel(ctx)
	cancel()
	if errors.Is(err, errUnknownModel) {
		logger.Error("check OPENROUTER_MODEL", "error", err)
		os.Exit(1)
	} else if err != nil {
		logger.Warn("model lookup failed; not reporting usage", "error", err)
	}

	manager := acp1.NewSessionManager(
		acp1.NewMemoryStore[*session](),
		func(_ context.Context, params *acp1.NewSessionRequest) (acp1.SessionID, *session, error) {
			return acp1.GenerateSessionID(), &session{cwd: params.Cwd, mode: askMode}, nil
		},
	)
	conn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return &openAgent{SessionManager: manager, client: c, llm: llm}
	}, os.Stdin, os.Stdout,
		acp.WithMiddleware(acp.LoggingMiddleware(logger)),
		acp.WithErrorHandler(func(err error) { logger.Error("acp", "error", err) }),
	)

	logger.Info("open-agent ready", "model", llm.model)
	if err := conn.Start(context.Background()); err != nil {
		logger.Error("connection ended", "error", err)
		os.Exit(1)
	}
}
