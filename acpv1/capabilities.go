package acpv1

import schema "github.com/ironpark/go-acp/schema/v1"

// CapabilitiesOf derives the agent capabilities implied by the optional
// interfaces agent implements, so an Initialize response cannot advertise a
// method the connection would answer with "method not found":
//
//	caps := acpv1.CapabilitiesOf(a)
//	caps.PromptCapabilities = &schema.PromptCapabilities{Image: new(true)}
//	return &acpv1.InitializeResponse{ProtocolVersion: acpv1.ProtocolVersion, AgentCapabilities: caps}, nil
//
// Group capabilities with their own sub-flags — providers and nes — are set to
// empty objects, which advertises the group; fill in the sub-flags the agent
// supports. [LogoutHandler] sets auth.logout. Prompt, MCP and position
// encoding capabilities describe content rather than methods and are left for
// the agent to set.
func CapabilitiesOf(agent Agent) *schema.AgentCapabilities {
	caps := &schema.AgentCapabilities{}
	if _, ok := agent.(SessionLoader); ok {
		caps.LoadSession = new(true)
	}
	session := &schema.SessionCapabilities{}
	advertise := false
	if _, ok := agent.(SessionLister); ok {
		session.List = &schema.SessionListCapabilities{}
		advertise = true
	}
	if _, ok := agent.(SessionDeleter); ok {
		session.Delete = &schema.SessionDeleteCapabilities{}
		advertise = true
	}
	if _, ok := agent.(SessionForker); ok {
		session.Fork = &schema.SessionForkCapabilities{}
		advertise = true
	}
	if _, ok := agent.(SessionResumer); ok {
		session.Resume = &schema.SessionResumeCapabilities{}
		advertise = true
	}
	if _, ok := agent.(SessionCloser); ok {
		session.Close = &schema.SessionCloseCapabilities{}
		advertise = true
	}
	if advertise {
		caps.SessionCapabilities = session
	}
	if _, ok := agent.(LogoutHandler); ok {
		caps.Auth = &schema.AgentAuthCapabilities{Logout: &schema.LogoutCapabilities{}}
	}
	if _, ok := agent.(ProviderManager); ok {
		caps.Providers = &schema.ProvidersCapabilities{}
	}
	if _, ok := agent.(NesHandler); ok {
		caps.Nes = &schema.NesCapabilities{}
	}
	return caps
}

// ClientCapabilitiesOf derives the client capabilities implied by the optional
// interfaces client implements: the fs flags from [FileReader] and
// [FileWriter], terminal from [TerminalHandler], and an empty elicitation
// object from [ElicitationHandler], whose form/url sub-flags the client fills
// in. Session, plan, auth, nes and position encoding capabilities describe
// what the client can display rather than which methods it serves and are
// left for the client to set.
func ClientCapabilitiesOf(client Client) *schema.ClientCapabilities {
	caps := &schema.ClientCapabilities{}
	_, read := client.(FileReader)
	_, write := client.(FileWriter)
	if read || write {
		caps.Fs = &schema.FileSystemCapabilities{ReadTextFile: &read, WriteTextFile: &write}
	}
	if _, ok := client.(TerminalHandler); ok {
		caps.Terminal = new(true)
	}
	if _, ok := client.(ElicitationHandler); ok {
		caps.Elicitation = &schema.ElicitationCapabilities{}
	}
	return caps
}
