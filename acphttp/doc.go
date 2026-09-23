// Package acphttp carries ACP over Streamable HTTP and WebSocket, following
// the draft RFD "Streamable HTTP & WebSocket Transport" that the TypeScript
// and Python SDKs implement.
//
// [NewServer] serves agents at one HTTP endpoint and hands each connection to
// a façade as an [acp.Transport]; [NewClientTransport] and [DialWebSocket]
// connect a client to such an endpoint:
//
//	http.Handle("/acp", acphttp.NewServer(func(ctx context.Context, t acp.Transport) error {
//		return acp1.NewAgentSideConnection(newAgent, t).Start(ctx)
//	}))
//
//	agent := acp1.ConnectAgent(ctx, acphttp.NewClientTransport("https://host/acp"), newClient)
//
// The wire behaviour tracks the RFD, which may still change. The package is
// separate from the root so that a stdio-only program does not link the HTTP
// and WebSocket code.
package acphttp
