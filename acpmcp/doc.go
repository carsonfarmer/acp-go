// Package acpmcp carries MCP over an ACP connection, so a client can hand an
// agent MCP servers that live in the client's own process, with no stdio
// child or HTTP port in between. It connects the official MCP Go SDK
// (github.com/modelcontextprotocol/go-sdk) to the mcp/connect, mcp/message
// and mcp/disconnect methods of ACP.
//
// The client side is a Host: it registers *mcp.Server values and lists them in
// session/new with the "acp" transport. The agent side is a Dialer: it opens
// an *mcp.ClientSession to one of those servers. Requests and notifications
// flow both ways, so server-to-client MCP features such as roots, sampling
// and list-changed notifications work too.
//
//	// client
//	host := acpmcp.NewHostV1(conn)
//	session, err := conn.NewSession(ctx, &acp1.NewSessionRequest{
//		Cwd:        cwd,
//		MCPServers: []acp1.MCPServer{host.Add("project-tools", server)},
//	})
//
//	// agent, for each MCPServerACP in the session/new request
//	tools, err := dialer.Connect(ctx, acpServer, mcp.NewClient(impl, nil), nil)
//	result, err := tools.CallTool(ctx, &mcp.CallToolParams{Name: "echo"})
//
// Host and Dialer exist for ACP v1 and v2; the wire format is the same.
//
// # Stability
//
// MCP-over-ACP is an RFD-stage draft of the protocol. The TypeScript and
// Python SDKs mark it unstable and the Rust SDK gates it behind the
// unstable_mcp_over_acp feature. Its wire format, and with it this package,
// may still change.
package acpmcp
