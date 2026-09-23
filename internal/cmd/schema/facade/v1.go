package facade

// V1 is the method table for the stable ACP v1 façade in acpv1.
var V1 = &Spec{
	Package:    "acpv1",
	Dir:        "acpv1",
	SchemaPath: "github.com/ironpark/go-acp/schema/v1",
	Unhandled: []string{
		// The reference SDKs leave MCP proxying to extensions in v1.
		"mcp/connect", "mcp/message", "mcp/disconnect",
	},
	ExtraTypes: []string{
		"SessionID", "SessionInfo", "SessionModeID", "SessionConfigOption", "SessionUpdate",
		"MessageID", "TerminalID", "ToolCallID", "ToolCallContent", "ToolCallLocation",
		"ToolCallStatus", "ToolKind", "ContentBlock", "PlanEntry", "AvailableCommand",
		"Cost", "StopReason", "Implementation", "PermissionOption", "PermissionOptionKind",
		"ToolCallUpdate", "RequestPermissionOutcome", "PlanEntryStatus", "PlanEntryPriority",
	},
	Agent: []Group{
		{
			Interface: "Agent",
			Required:  true,
			Doc: `Agent is the set of methods every ACP agent must handle.

Everything beyond these four methods is optional and gated by a capability
the agent advertises from [Agent.Initialize]. Implement the matching optional
interface and the connection routes the method to it; when it is not
implemented the peer receives "method not found".

See protocol docs: [Agent](https://agentclientprotocol.com/protocol/overview#agent)`,
			Methods: []Method{
				{
					Wire: "initialize", Name: "Initialize", Params: "InitializeRequest", Response: "InitializeResponse",
					Doc: `Initialize negotiates the protocol version and exchanges capabilities.

See protocol docs: [Initialization](https://agentclientprotocol.com/protocol/initialization)`,
					CallDoc: `Initialize negotiates the protocol version and exchanges capabilities. It is
the first call on every connection.`,
				},
				{
					Wire: "session/new", Name: "NewSession", Params: "NewSessionRequest", Response: "NewSessionResponse",
					Doc: `NewSession creates a conversation session with its own context.

See protocol docs: [Session Setup](https://agentclientprotocol.com/protocol/session-setup)`,
					CallDoc: `NewSession creates a session. It may fail with an auth-required error.`,
				},
				{
					Wire: "session/prompt", Name: "Prompt", Params: "PromptRequest", Response: "PromptResponse",
					Doc: `Prompt runs one prompt turn and returns once it stops.

See protocol docs: [Prompt Turn](https://agentclientprotocol.com/protocol/prompt-turn)`,
					CallDoc: `Prompt runs one prompt turn and returns once the agent stops.

Cancelling ctx cancels the JSON-RPC request; to cancel the turn itself with
the protocol's own semantics, send [ClientSideConnection.Cancel].

See protocol docs: [Prompt Turn](https://agentclientprotocol.com/protocol/prompt-turn)`,
				},
				{
					Wire: "session/cancel", Name: "Cancel", Params: "CancelNotification",
					Doc: `Cancel is a notification asking the agent to abort the current turn.
The pending Prompt call should return with StopReasonCancelled.

See protocol docs: [Cancellation](https://agentclientprotocol.com/protocol/prompt-turn#cancellation)`,
					CallDoc: `Cancel asks the agent to end the current turn. The pending Prompt call
returns with the cancelled stop reason.

See protocol docs: [Cancellation](https://agentclientprotocol.com/protocol/prompt-turn#cancellation)`,
				},
			},
		},
		{
			Interface: "Authenticator",
			Doc: `Authenticator handles authenticate. Implement it when the agent lists
` + "`authMethods`" + ` in its Initialize response; an agent that needs no
credentials leaves it out and the method answers "method not found".

See protocol docs: [Authentication](https://agentclientprotocol.com/protocol/authentication)`,
			Methods: []Method{{
				Wire: "authenticate", Name: "Authenticate", Params: "AuthenticateRequest", Response: "AuthenticateResponse",
				CallDoc: `Authenticate authenticates with one of the methods the agent advertised.`,
			}},
		},
		{
			Interface: "SessionLoader",
			Doc: `SessionLoader handles session/load. Advertise it with the ` + "`loadSession`" + `
agent capability. [SessionManager] provides an implementation.`,
			Methods: []Method{{
				Wire: "session/load", Name: "LoadSession", Params: "LoadSessionRequest", Response: "LoadSessionResponse",
				CallDoc: `LoadSession resumes a session and replays its history as notifications.
Requires the agent's ` + "`loadSession`" + ` capability.`,
			}},
		},
		{
			Interface: "SessionLister",
			Doc: `SessionLister handles session/list. Advertise it with the
` + "`sessionCapabilities.list`" + ` agent capability.`,
			Methods: []Method{{
				Wire: "session/list", Name: "ListSessions", Params: "ListSessionsRequest", Response: "ListSessionsResponse",
				CallDoc: `ListSessions lists sessions, optionally filtered and paginated.`,
			}},
		},
		{
			Interface: "SessionDeleter",
			Doc: `SessionDeleter handles session/delete. Advertise it with the
` + "`sessionCapabilities.delete`" + ` agent capability.`,
			Methods: []Method{{
				Wire: "session/delete", Name: "DeleteSession", Params: "DeleteSessionRequest", Response: "DeleteSessionResponse",
				CallDoc: `DeleteSession deletes a session and its stored history.`,
			}},
		},
		{
			Interface:    "SessionForker",
			Experimental: true,
			Doc: `SessionForker handles session/fork. Advertise it with the
` + "`sessionCapabilities.fork`" + ` agent capability.`,
			Methods: []Method{{
				Wire: "session/fork", Name: "ForkSession", Params: "ForkSessionRequest", Response: "ForkSessionResponse",
				CallDoc: `ForkSession branches a session so work continues without touching the
original history.`,
			}},
		},
		{
			Interface:    "SessionResumer",
			Experimental: true,
			Doc: `SessionResumer handles session/resume, continuing a session without
replaying its history. Advertise it with the ` + "`sessionCapabilities.resume`" + `
agent capability.`,
			Methods: []Method{{
				Wire: "session/resume", Name: "ResumeSession", Params: "ResumeSessionRequest", Response: "ResumeSessionResponse",
				CallDoc: `ResumeSession continues a session without replaying its history.`,
			}},
		},
		{
			Interface:    "SessionCloser",
			Experimental: true,
			Doc: `SessionCloser handles session/close. Advertise it with the
` + "`sessionCapabilities.close`" + ` agent capability.`,
			Methods: []Method{{
				Wire: "session/close", Name: "CloseSession", Params: "CloseSessionRequest", Response: "CloseSessionResponse",
				CallDoc: `CloseSession cancels any ongoing work and frees the session's resources.`,
			}},
		},
		{
			Interface: "SessionModeSetter",
			Doc: `SessionModeSetter handles session/set_mode.

See protocol docs: [Session Modes](https://agentclientprotocol.com/protocol/session-modes)`,
			Methods: []Method{{
				Wire: "session/set_mode", Name: "SetSessionMode", Params: "SetSessionModeRequest", Response: "SetSessionModeResponse",
				CallDoc: `SetSessionMode switches the session between the agent's advertised modes.`,
			}},
		},
		{
			Interface: "SessionConfigOptionSetter",
			Doc: `SessionConfigOptionSetter handles session/set_config_option. The response
carries every option and its current value, since changing one option may
change the others.`,
			Methods: []Method{{
				Wire: "session/set_config_option", Name: "SetSessionConfigOption", Params: "SetSessionConfigOptionRequest", Response: "SetSessionConfigOptionResponse",
				CallDoc: `SetSessionConfigOption sets one configuration option. The response returns
every option, since one change may affect the others.`,
			}},
		},
		{
			Interface:    "ProviderManager",
			Experimental: true,
			Doc: `ProviderManager handles the providers/* methods. Advertise them with the
` + "`providers`" + ` agent capability.`,
			Methods: []Method{
				{Wire: "providers/list", Name: "ListProviders", Params: "ListProvidersRequest", Response: "ListProvidersResponse",
					CallDoc: "ListProviders lists the model providers the agent can use."},
				{Wire: "providers/set", Name: "SetProvider", Params: "SetProviderRequest", Response: "SetProviderResponse",
					CallDoc: "SetProvider configures one provider."},
				{Wire: "providers/disable", Name: "DisableProvider", Params: "DisableProviderRequest", Response: "DisableProviderResponse",
					CallDoc: "DisableProvider turns one provider off."},
			},
		},
		{
			Interface: "LogoutHandler",
			Doc:       `LogoutHandler handles the logout method, clearing stored credentials.`,
			Methods: []Method{{
				Wire: "logout", Name: "Logout", Params: "LogoutRequest", Response: "LogoutResponse",
				CallDoc: `Logout clears the credentials the agent holds.`,
			}},
		},
		{
			Interface:    "NesHandler",
			Experimental: true,
			Doc: `NesHandler handles the nes/* methods for Next Edit Suggestions. Advertise
them with the ` + "`nes`" + ` agent capability. AcceptNes and RejectNes are
notifications.`,
			Methods: nesMethods,
		},
		{
			Interface:    "DocumentHandler",
			Experimental: true,
			Doc: `DocumentHandler receives the document/did* notifications that mirror the
client's open editors.`,
			Methods: documentMethods,
		},
	},
	Client: []Group{
		{
			Interface: "Client",
			Required:  true,
			Doc: `Client is the set of methods every ACP client must handle.

File system, terminal and elicitation support are optional; implement the
matching interface and advertise the capability from
` + "`InitializeRequest.ClientCapabilities`" + `.

See protocol docs: [Client](https://agentclientprotocol.com/protocol/overview#client)`,
			Methods: []Method{
				{
					Wire: "session/update", Name: "SessionUpdate", Params: "SessionNotification", Via: "sessionUpdate",
					Doc: `SessionUpdate is a notification streaming turn progress to the user.

Notifications are handled one at a time on the connection's read loop, which
keeps updates in order and ahead of the prompt response. The flip side: a
handler that calls the agent and waits for the answer blocks the loop that
would read it. Hand such calls to a goroutine.

See protocol docs: [Agent Reports Output](https://agentclientprotocol.com/protocol/prompt-turn#3-agent-reports-output)`,
					CallDoc: `SessionUpdate streams turn progress to the client.`,
				},
				{
					Wire: "session/request_permission", Name: "RequestPermission", Params: "RequestPermissionRequest", Response: "RequestPermissionResponse",
					Doc: `RequestPermission asks the user to authorize a tool call.

When the turn is cancelled the client MUST answer with the cancelled
outcome rather than leaving the request pending.

See protocol docs: [Requesting Permission](https://agentclientprotocol.com/protocol/tool-calls#requesting-permission)`,
					CallDoc: `RequestPermission asks the user to authorize a tool call.`,
				},
			},
		},
		{
			Interface: "FileReader",
			Doc: `FileReader handles fs/read_text_file. Advertise it with the
` + "`fs.readTextFile`" + ` client capability.`,
			Methods: []Method{{
				Wire: "fs/read_text_file", Name: "ReadTextFile", Params: "ReadTextFileRequest", Response: "ReadTextFileResponse",
				CallDoc: `ReadTextFile reads a text file through the client. Requires the client's
` + "`fs.readTextFile`" + ` capability.`,
			}},
		},
		{
			Interface: "FileWriter",
			Doc: `FileWriter handles fs/write_text_file. Advertise it with the
` + "`fs.writeTextFile`" + ` client capability.`,
			Methods: []Method{{
				Wire: "fs/write_text_file", Name: "WriteTextFile", Params: "WriteTextFileRequest", Response: "WriteTextFileResponse",
				CallDoc: `WriteTextFile writes a text file through the client. Requires the client's
` + "`fs.writeTextFile`" + ` capability.`,
			}},
		},
		{
			Interface: "TerminalHandler",
			Doc: `TerminalHandler handles every terminal/* method. Advertise it with the
` + "`terminal`" + ` client capability, which covers all five methods at once.

See protocol docs: [Terminals](https://agentclientprotocol.com/protocol/terminals)`,
			Methods: []Method{
				{Wire: "terminal/create", Name: "CreateTerminal", Params: "CreateTerminalRequest", Response: "CreateTerminalResponse",
					CallDoc: `CreateTerminal starts a command in a client-managed terminal. Requires the
client's ` + "`terminal`" + ` capability. [AgentSideConnection.NewTerminal] also returns
a handle bound to the new terminal.`},
				{Wire: "terminal/output", Name: "TerminalOutput", Params: "TerminalOutputRequest", Response: "TerminalOutputResponse",
					CallDoc: `TerminalOutput returns a terminal's output so far without waiting for exit.`},
				{Wire: "terminal/release", Name: "ReleaseTerminal", Params: "ReleaseTerminalRequest", Response: "ReleaseTerminalResponse",
					CallDoc: `ReleaseTerminal kills the command if needed and frees the terminal.`},
				{Wire: "terminal/wait_for_exit", Name: "WaitForTerminalExit", Params: "WaitForTerminalExitRequest", Response: "WaitForTerminalExitResponse",
					CallDoc: `WaitForTerminalExit blocks until the terminal's command exits.`},
				{Wire: "terminal/kill", Name: "KillTerminal", Params: "KillTerminalRequest", Response: "KillTerminalResponse",
					CallDoc: `KillTerminal kills the command but keeps the terminal id valid.`},
			},
		},
		{
			Interface: "ElicitationHandler",
			Doc: `ElicitationHandler handles elicitation/create and the elicitation/complete
notification. Advertise it with the ` + "`elicitation`" + ` client capability.`,
			Methods: elicitationMethods("elicitation"),
		},
	},
}

// nesMethods is shared by both versions; the wire methods and types match.
var nesMethods = []Method{
	{Wire: "nes/start", Name: "StartNes", Params: "StartNesRequest", Response: "StartNesResponse",
		CallDoc: "StartNes starts a Next Edit Suggestions stream."},
	{Wire: "nes/suggest", Name: "SuggestNes", Params: "SuggestNesRequest", Response: "SuggestNesResponse",
		CallDoc: "SuggestNes asks for the next edit suggestion."},
	{Wire: "nes/close", Name: "CloseNes", Params: "CloseNesRequest", Response: "CloseNesResponse",
		CallDoc: "CloseNes ends a Next Edit Suggestions stream."},
	{Wire: "nes/accept", Name: "AcceptNes", Params: "AcceptNesNotification",
		CallDoc: "AcceptNes reports that the user accepted a suggestion."},
	{Wire: "nes/reject", Name: "RejectNes", Params: "RejectNesNotification",
		CallDoc: "RejectNes reports that the user rejected a suggestion."},
}

var documentMethods = []Method{
	{Wire: "document/didOpen", Name: "DidOpenDocument", Params: "DidOpenDocumentNotification",
		CallDoc: "DidOpenDocument tells the agent a document was opened."},
	{Wire: "document/didChange", Name: "DidChangeDocument", Params: "DidChangeDocumentNotification",
		CallDoc: "DidChangeDocument tells the agent a document changed."},
	{Wire: "document/didClose", Name: "DidCloseDocument", Params: "DidCloseDocumentNotification",
		CallDoc: "DidCloseDocument tells the agent a document was closed."},
	{Wire: "document/didSave", Name: "DidSaveDocument", Params: "DidSaveDocumentNotification",
		CallDoc: "DidSaveDocument tells the agent a document was saved."},
	{Wire: "document/didFocus", Name: "DidFocusDocument", Params: "DidFocusDocumentNotification",
		CallDoc: "DidFocusDocument tells the agent a document was focused."},
}

// elicitationMethods takes the capability path, which differs between versions.
func elicitationMethods(capability string) []Method {
	return []Method{
		{Wire: "elicitation/create", Name: "CreateElicitation", Params: "CreateElicitationRequest", Response: "CreateElicitationResponse",
			CallDoc: `CreateElicitation asks the client to collect input from the user. Requires
the client's ` + "`" + capability + "`" + ` capability.`},
		{Wire: "elicitation/complete", Name: "CompleteElicitation", Params: "CompleteElicitationNotification",
			CallDoc: `CompleteElicitation tells the client an elicitation no longer needs an answer.`},
	}
}
