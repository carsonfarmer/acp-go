package acp

import (
	"context"

	schema "github.com/ironpark/go-acp/schema/v1"
)

// SendOption sets optional fields on an outgoing session update.
type SendOption func(*sendOptions)

type sendOptions struct {
	messageID *MessageID
}

// WithMessageID groups consecutive chunks into one logical message.
func WithMessageID(id MessageID) SendOption {
	return func(o *sendOptions) { o.messageID = &id }
}

func applySendOptions(opts []SendOption) sendOptions {
	var o sendOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// SessionStream sends session/update notifications for one session.
//
// It removes the boilerplate of naming the session and building a
// [SessionUpdate] variant for each chunk:
//
//	stream := acp.NewSessionStream(client, sessionID)
//	stream.SendText(ctx, "Reading the file…")
//	stream.StartToolCall(ctx, toolID, "Read file", schema.ToolKindRead)
//	stream.CompleteToolCall(ctx, toolID)
//
// Use [SessionStream.Send] for any update the helpers do not cover.
type SessionStream struct {
	client    Client
	sessionID SessionID
}

// NewSessionStream binds a client to a session id.
func NewSessionStream(client Client, sessionID SessionID) *SessionStream {
	return &SessionStream{client: client, sessionID: sessionID}
}

// SessionID returns the session this stream reports on.
func (s *SessionStream) SessionID() SessionID { return s.sessionID }

// SendText streams agent message text.
func (s *SessionStream) SendText(ctx context.Context, text string, opts ...SendOption) error {
	o := applySendOptions(opts)
	return s.Send(ctx, schema.NewSessionUpdate(schema.SessionUpdateAgentMessageChunk{
		Content:   textBlock(text),
		MessageID: o.messageID,
	}))
}

// SendThought streams the agent's reasoning, which clients display separately
// from its message.
func (s *SessionStream) SendThought(ctx context.Context, text string, opts ...SendOption) error {
	o := applySendOptions(opts)
	return s.Send(ctx, schema.NewSessionUpdate(schema.SessionUpdateAgentThoughtChunk{
		Content:   textBlock(text),
		MessageID: o.messageID,
	}))
}

// SendUserMessage echoes user message text, which agents use when replaying a
// loaded session's history.
func (s *SessionStream) SendUserMessage(ctx context.Context, text string, opts ...SendOption) error {
	o := applySendOptions(opts)
	return s.Send(ctx, schema.NewSessionUpdate(schema.SessionUpdateUserMessageChunk{
		Content:   textBlock(text),
		MessageID: o.messageID,
	}))
}

// SendContent streams an arbitrary content block as an agent message chunk,
// for images, audio and embedded resources.
func (s *SessionStream) SendContent(ctx context.Context, content ContentBlock, opts ...SendOption) error {
	o := applySendOptions(opts)
	return s.Send(ctx, schema.NewSessionUpdate(schema.SessionUpdateAgentMessageChunk{
		Content:   content,
		MessageID: o.messageID,
	}))
}

// StartToolCall reports a tool call that is now running.
func (s *SessionStream) StartToolCall(ctx context.Context, id ToolCallID, title string, kind ToolKind, locations ...ToolCallLocation) error {
	status := schema.ToolCallStatusInProgress
	return s.Send(ctx, schema.NewSessionUpdate(schema.SessionUpdateToolCall{
		ToolCallID: id,
		Title:      title,
		Kind:       &kind,
		Status:     &status,
		Locations:  locations,
	}))
}

// UpdateToolCallStatus moves a tool call to another status.
func (s *SessionStream) UpdateToolCallStatus(ctx context.Context, id ToolCallID, status ToolCallStatus) error {
	return s.Send(ctx, schema.NewSessionUpdate(schema.SessionUpdateToolCallUpdate{
		ToolCallID: id,
		Status:     &status,
	}))
}

// CompleteToolCall marks a tool call completed, with its output if any.
func (s *SessionStream) CompleteToolCall(ctx context.Context, id ToolCallID, content ...ToolCallContent) error {
	status := schema.ToolCallStatusCompleted
	return s.Send(ctx, schema.NewSessionUpdate(schema.SessionUpdateToolCallUpdate{
		ToolCallID: id,
		Status:     &status,
		Content:    content,
	}))
}

// FailToolCall marks a tool call failed, with any error output.
func (s *SessionStream) FailToolCall(ctx context.Context, id ToolCallID, content ...ToolCallContent) error {
	status := schema.ToolCallStatusFailed
	return s.Send(ctx, schema.NewSessionUpdate(schema.SessionUpdateToolCallUpdate{
		ToolCallID: id,
		Status:     &status,
		Content:    content,
	}))
}

// SendPlan reports the agent's plan for the turn.
func (s *SessionStream) SendPlan(ctx context.Context, entries []PlanEntry) error {
	return s.Send(ctx, schema.NewSessionUpdate(schema.SessionUpdatePlan{Entries: entries}))
}

// SendModeUpdate reports that the agent switched session mode on its own.
func (s *SessionStream) SendModeUpdate(ctx context.Context, modeID SessionModeID) error {
	return s.Send(ctx, schema.NewSessionUpdate(schema.SessionUpdateCurrentModeUpdate{CurrentModeID: modeID}))
}

// SendConfigUpdate reports new values for the session's config options.
func (s *SessionStream) SendConfigUpdate(ctx context.Context, options []SessionConfigOption) error {
	return s.Send(ctx, schema.NewSessionUpdate(schema.SessionUpdateConfigOptionUpdate{ConfigOptions: options}))
}

// SendCommands reports the slash commands available in this session.
func (s *SessionStream) SendCommands(ctx context.Context, commands []AvailableCommand) error {
	return s.Send(ctx, schema.NewSessionUpdate(schema.SessionUpdateAvailableCommandsUpdate{AvailableCommands: commands}))
}

// SendUsage reports context window usage for the turn so far: used tokens out
// of size, with an optional running cost.
func (s *SessionStream) SendUsage(ctx context.Context, used, size float64, cost *Cost) error {
	return s.Send(ctx, schema.NewSessionUpdate(schema.SessionUpdateUsageUpdate{
		Used: used,
		Size: size,
		Cost: cost,
	}))
}

// Send sends any session update, including variants without a helper.
func (s *SessionStream) Send(ctx context.Context, update SessionUpdate) error {
	return s.client.SessionUpdate(ctx, &SessionNotification{
		SessionID: s.sessionID,
		Update:    update,
	})
}

func textBlock(text string) ContentBlock {
	return schema.NewContentBlock(schema.ContentBlockText{Text: text})
}
