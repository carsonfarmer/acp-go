package acpv2

import (
	"context"

	schema "github.com/ironpark/go-acp/schema/v2"
)

// SessionStream sends session/update notifications for one session.
//
// It removes the boilerplate of naming the session and building a
// [SessionUpdate] variant for each update. v2 keys every message by id and
// makes the turn state explicit, so a typical turn reads:
//
//	stream := acpv2.NewSessionStream(client, sessionID)
//	stream.Running(ctx)
//	stream.SendText(ctx, messageID, "Reading the file…")
//	stream.StartToolCall(ctx, toolID, "Read file", acpv2.ToolKindRead)
//	stream.CompleteToolCall(ctx, toolID, acpv2.ToolText(contents))
//	stream.Idle(ctx, schema.StopReasonEndTurn)
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

// SendText appends text to the agent message with the given id.
func (s *SessionStream) SendText(ctx context.Context, id MessageID, text string) error {
	return s.SendContent(ctx, id, TextBlock(text))
}

// SendContent appends a content block to the agent message with the given id,
// for images, audio and embedded resources.
func (s *SessionStream) SendContent(ctx context.Context, id MessageID, content ContentBlock) error {
	return s.send(ctx, schema.SessionUpdateAgentMessageChunk{MessageID: id, Content: content})
}

// SendThought appends text to the agent's reasoning with the given id, which
// clients display separately from its messages.
func (s *SessionStream) SendThought(ctx context.Context, id MessageID, text string) error {
	return s.send(ctx, schema.SessionUpdateAgentThoughtChunk{MessageID: id, Content: TextBlock(text)})
}

// SendUserMessage reports a user message in full: the message a prompt
// inserted, which v2 agents must echo, or history replayed on resume.
func (s *SessionStream) SendUserMessage(ctx context.Context, id MessageID, content ...ContentBlock) error {
	return s.send(ctx, schema.SessionUpdateUserMessage{MessageID: id, Content: content})
}

// StartToolCall reports a tool call that is now running.
func (s *SessionStream) StartToolCall(ctx context.Context, id ToolCallID, title string, kind ToolKind, locations ...ToolCallLocation) error {
	return s.send(ctx, schema.SessionUpdateToolCallUpdate{
		ToolCallID: id,
		Title:      &title,
		Kind:       &kind,
		Status:     new(schema.ToolCallStatusInProgress),
		Locations:  locations,
	})
}

// UpdateToolCallStatus moves a tool call to another status.
func (s *SessionStream) UpdateToolCallStatus(ctx context.Context, id ToolCallID, status ToolCallStatus) error {
	return s.send(ctx, schema.SessionUpdateToolCallUpdate{ToolCallID: id, Status: &status})
}

// SendToolOutput appends output to a running tool call.
func (s *SessionStream) SendToolOutput(ctx context.Context, id ToolCallID, content ToolCallContent) error {
	return s.send(ctx, schema.SessionUpdateToolCallContentChunk{ToolCallID: id, Content: content})
}

// CompleteToolCall marks a tool call completed, replacing its content with
// the given output if any.
func (s *SessionStream) CompleteToolCall(ctx context.Context, id ToolCallID, content ...ToolCallContent) error {
	return s.send(ctx, schema.SessionUpdateToolCallUpdate{
		ToolCallID: id,
		Status:     new(schema.ToolCallStatusCompleted),
		Content:    content,
	})
}

// FailToolCall marks a tool call failed, replacing its content with any error
// output.
func (s *SessionStream) FailToolCall(ctx context.Context, id ToolCallID, content ...ToolCallContent) error {
	return s.send(ctx, schema.SessionUpdateToolCallUpdate{
		ToolCallID: id,
		Status:     new(schema.ToolCallStatusFailed),
		Content:    content,
	})
}

// SendCommands reports the slash commands available in this session.
func (s *SessionStream) SendCommands(ctx context.Context, commands []AvailableCommand) error {
	return s.send(ctx, schema.SessionUpdateAvailableCommandsUpdate{AvailableCommands: commands})
}

// SendConfigUpdate reports new values for the session's config options.
func (s *SessionStream) SendConfigUpdate(ctx context.Context, options []SessionConfigOption) error {
	return s.send(ctx, schema.SessionUpdateConfigOptionUpdate{ConfigOptions: options})
}

// SendUsage reports context window usage: used tokens out of size, with an
// optional running cost.
func (s *SessionStream) SendUsage(ctx context.Context, used, size float64, cost *Cost) error {
	return s.send(ctx, schema.SessionUpdateUsageUpdate{Used: used, Size: size, Cost: cost})
}

// Running reports that foreground work started or resumed.
func (s *SessionStream) Running(ctx context.Context) error {
	return s.state(ctx, schema.StateUpdateRunning{})
}

// RequiresAction reports that foreground work is blocked on the user, such as
// a pending permission request.
func (s *SessionStream) RequiresAction(ctx context.Context) error {
	return s.state(ctx, schema.StateUpdateRequiresAction{})
}

// Idle reports that the agent is ready for a new prompt, ending the turn with
// the given reason. Clients treat this as the end of the turn.
func (s *SessionStream) Idle(ctx context.Context, reason StopReason) error {
	return s.state(ctx, schema.StateUpdateIdle{StopReason: &reason})
}

// Send sends any session update, including variants without a helper.
func (s *SessionStream) Send(ctx context.Context, update SessionUpdate) error {
	return s.client.SessionUpdate(ctx, &UpdateSessionNotification{
		SessionID: s.sessionID,
		Update:    update,
	})
}

func (s *SessionStream) send(ctx context.Context, v schema.SessionUpdateVariant) error {
	return s.Send(ctx, schema.NewSessionUpdate(v))
}

func (s *SessionStream) state(ctx context.Context, v schema.StateUpdateVariant) error {
	return s.send(ctx, schema.SessionUpdateStateUpdate{Value: schema.NewStateUpdate(v)})
}
