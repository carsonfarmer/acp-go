package acpv2

import schema "github.com/ironpark/go-acp/schema/v2"

// This file re-exports the wire types that appear in this package's own API so
// that a caller implementing [Agent] or [Client] needs one import. Every other
// generated type — content blocks, session updates, capabilities, unions and
// their constructors — lives in the schema package:
//
//	import schema "github.com/ironpark/go-acp/schema/v2"

// ProtocolVersion is the ACP protocol version implemented by this package.
const ProtocolVersion = schema.CurrentProtocolVersion

type (
	AcceptNesNotification           = schema.AcceptNesNotification
	CancelRequestNotification       = schema.CancelRequestNotification
	CancelSessionNotification       = schema.CancelSessionNotification
	CloseNesRequest                 = schema.CloseNesRequest
	CloseNesResponse                = schema.CloseNesResponse
	CloseSessionRequest             = schema.CloseSessionRequest
	CloseSessionResponse            = schema.CloseSessionResponse
	CompleteElicitationNotification = schema.CompleteElicitationNotification
	ConnectMCPRequest               = schema.ConnectMCPRequest
	ConnectMCPResponse              = schema.ConnectMCPResponse
	CreateElicitationRequest        = schema.CreateElicitationRequest
	CreateElicitationResponse       = schema.CreateElicitationResponse
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
	ListProvidersRequest            = schema.ListProvidersRequest
	ListProvidersResponse           = schema.ListProvidersResponse
	ListSessionsRequest             = schema.ListSessionsRequest
	ListSessionsResponse            = schema.ListSessionsResponse
	LoginAuthRequest                = schema.LoginAuthRequest
	LoginAuthResponse               = schema.LoginAuthResponse
	LogoutAuthRequest               = schema.LogoutAuthRequest
	LogoutAuthResponse              = schema.LogoutAuthResponse
	MessageMCPNotification          = schema.MessageMCPNotification
	MessageMCPRequest               = schema.MessageMCPRequest
	MessageMCPResponse              = schema.MessageMCPResponse
	NewSessionRequest               = schema.NewSessionRequest
	NewSessionResponse              = schema.NewSessionResponse
	PromptRequest                   = schema.PromptRequest
	PromptResponse                  = schema.PromptResponse
	RejectNesNotification           = schema.RejectNesNotification
	RequestPermissionRequest        = schema.RequestPermissionRequest
	RequestPermissionResponse       = schema.RequestPermissionResponse
	ResumeSessionRequest            = schema.ResumeSessionRequest
	ResumeSessionResponse           = schema.ResumeSessionResponse
	SetProviderRequest              = schema.SetProviderRequest
	SetProviderResponse             = schema.SetProviderResponse
	SetSessionConfigOptionRequest   = schema.SetSessionConfigOptionRequest
	SetSessionConfigOptionResponse  = schema.SetSessionConfigOptionResponse
	StartNesRequest                 = schema.StartNesRequest
	StartNesResponse                = schema.StartNesResponse
	SuggestNesRequest               = schema.SuggestNesRequest
	SuggestNesResponse              = schema.SuggestNesResponse
	UpdateSessionNotification       = schema.UpdateSessionNotification

	// Identifiers and values shared across those payloads.
	SessionID           = schema.SessionID
	SessionInfo         = schema.SessionInfo
	SessionConfigOption = schema.SessionConfigOption
	SessionUpdate       = schema.SessionUpdate
	MessageID           = schema.MessageID
	ToolCallID          = schema.ToolCallID
	ContentBlock        = schema.ContentBlock
	AbsolutePath        = schema.AbsolutePath
	MCPConnectionID     = schema.MCPConnectionID
	MCPServerACPID      = schema.MCPServerACPID
	StopReason          = schema.StopReason
)
