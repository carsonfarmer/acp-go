package acp

import schema "github.com/ironpark/go-acp/schema/v1"

// This file re-exports the wire types that appear in this package's own API so
// that a caller implementing [Agent] or [Client] needs one import. Every other
// generated type — content blocks, session updates, capabilities, unions and
// their constructors — lives in the schema package:
//
//	import schema "github.com/ironpark/go-acp/schema/v1"

// ProtocolVersion is the ACP protocol version implemented by this package.
const ProtocolVersion = schema.CurrentProtocolVersion

type (
	AcceptNesNotification           = schema.AcceptNesNotification
	AuthenticateRequest             = schema.AuthenticateRequest
	AuthenticateResponse            = schema.AuthenticateResponse
	CancelNotification              = schema.CancelNotification
	CancelRequestNotification       = schema.CancelRequestNotification
	CloseNesRequest                 = schema.CloseNesRequest
	CloseNesResponse                = schema.CloseNesResponse
	CloseSessionRequest             = schema.CloseSessionRequest
	CloseSessionResponse            = schema.CloseSessionResponse
	CompleteElicitationNotification = schema.CompleteElicitationNotification
	ConnectMCPRequest               = schema.ConnectMCPRequest
	ConnectMCPResponse              = schema.ConnectMCPResponse
	CreateElicitationRequest        = schema.CreateElicitationRequest
	CreateElicitationResponse       = schema.CreateElicitationResponse
	CreateTerminalRequest           = schema.CreateTerminalRequest
	CreateTerminalResponse          = schema.CreateTerminalResponse
	DeleteSessionRequest            = schema.DeleteSessionRequest
	DeleteSessionResponse           = schema.DeleteSessionResponse
	DidChangeDocumentNotification   = schema.DidChangeDocumentNotification
	DidCloseDocumentNotification    = schema.DidCloseDocumentNotification
	DidFocusDocumentNotification    = schema.DidFocusDocumentNotification
	DidOpenDocumentNotification     = schema.DidOpenDocumentNotification
	DidSaveDocumentNotification     = schema.DidSaveDocumentNotification
	DisableProviderRequest          = schema.DisableProviderRequest
	DisableProviderResponse         = schema.DisableProviderResponse
	DisconnectMCPRequest            = schema.DisconnectMCPRequest
	DisconnectMCPResponse           = schema.DisconnectMCPResponse
	ExtNotification                 = schema.ExtNotification
	ExtRequest                      = schema.ExtRequest
	ExtResponse                     = schema.ExtResponse
	ForkSessionRequest              = schema.ForkSessionRequest
	ForkSessionResponse             = schema.ForkSessionResponse
	InitializeRequest               = schema.InitializeRequest
	InitializeResponse              = schema.InitializeResponse
	KillTerminalRequest             = schema.KillTerminalRequest
	KillTerminalResponse            = schema.KillTerminalResponse
	ListProvidersRequest            = schema.ListProvidersRequest
	ListProvidersResponse           = schema.ListProvidersResponse
	ListSessionsRequest             = schema.ListSessionsRequest
	ListSessionsResponse            = schema.ListSessionsResponse
	LoadSessionRequest              = schema.LoadSessionRequest
	LoadSessionResponse             = schema.LoadSessionResponse
	LogoutRequest                   = schema.LogoutRequest
	LogoutResponse                  = schema.LogoutResponse
	MessageMCPNotification          = schema.MessageMCPNotification
	MessageMCPRequest               = schema.MessageMCPRequest
	MessageMCPResponse              = schema.MessageMCPResponse
	NewSessionRequest               = schema.NewSessionRequest
	NewSessionResponse              = schema.NewSessionResponse
	PromptRequest                   = schema.PromptRequest
	PromptResponse                  = schema.PromptResponse
	ReadTextFileRequest             = schema.ReadTextFileRequest
	ReadTextFileResponse            = schema.ReadTextFileResponse
	RejectNesNotification           = schema.RejectNesNotification
	ReleaseTerminalRequest          = schema.ReleaseTerminalRequest
	ReleaseTerminalResponse         = schema.ReleaseTerminalResponse
	RequestPermissionRequest        = schema.RequestPermissionRequest
	RequestPermissionResponse       = schema.RequestPermissionResponse
	ResumeSessionRequest            = schema.ResumeSessionRequest
	ResumeSessionResponse           = schema.ResumeSessionResponse
	SessionNotification             = schema.SessionNotification
	SetProviderRequest              = schema.SetProviderRequest
	SetProviderResponse             = schema.SetProviderResponse
	SetSessionConfigOptionRequest   = schema.SetSessionConfigOptionRequest
	SetSessionConfigOptionResponse  = schema.SetSessionConfigOptionResponse
	SetSessionModeRequest           = schema.SetSessionModeRequest
	SetSessionModeResponse          = schema.SetSessionModeResponse
	StartNesRequest                 = schema.StartNesRequest
	StartNesResponse                = schema.StartNesResponse
	SuggestNesRequest               = schema.SuggestNesRequest
	SuggestNesResponse              = schema.SuggestNesResponse
	TerminalOutputRequest           = schema.TerminalOutputRequest
	TerminalOutputResponse          = schema.TerminalOutputResponse
	WaitForTerminalExitRequest      = schema.WaitForTerminalExitRequest
	WaitForTerminalExitResponse     = schema.WaitForTerminalExitResponse
	WriteTextFileRequest            = schema.WriteTextFileRequest
	WriteTextFileResponse           = schema.WriteTextFileResponse

	// Identifiers and values shared across those payloads, and the types the
	// SessionStream and SessionManager helpers take.
	SessionID           = schema.SessionID
	SessionInfo         = schema.SessionInfo
	SessionModeID       = schema.SessionModeID
	SessionConfigOption = schema.SessionConfigOption
	SessionUpdate       = schema.SessionUpdate
	MessageID           = schema.MessageID
	TerminalID          = schema.TerminalID
	ToolCallID          = schema.ToolCallID
	ToolCallContent     = schema.ToolCallContent
	ToolCallLocation    = schema.ToolCallLocation
	ToolCallStatus      = schema.ToolCallStatus
	ToolKind            = schema.ToolKind
	ContentBlock        = schema.ContentBlock
	PlanEntry           = schema.PlanEntry
	AvailableCommand    = schema.AvailableCommand
	Cost                = schema.Cost
	StopReason          = schema.StopReason
)
