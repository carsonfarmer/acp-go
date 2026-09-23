// Package acp2 implements the draft Agent Client Protocol v2 for Go.
//
// ACP v2 is still a draft: its wire protocol and this API may change
// incompatibly in any release. The stable protocol is [github.com/ironpark/acp-go/acp1];
// import this package to opt in, exactly as the upstream TypeScript SDK
// exposes v2 under an experimental entry point.
//
// The wire types live in [github.com/ironpark/acp-go/schema/v2]; this package
// adds the [AgentSideConnection] and [ClientSideConnection] façades on the
// shared runtime in [github.com/ironpark/acp-go], which also provides the
// options, transports, middleware and error type both versions use.
//
// Compared with v1, v2 moves file system and terminal access behind MCP
// (`mcp/*`), replaces `authenticate` with `auth/login`/`auth/logout`, drops
// `session/load` and `session/set_mode`, and renames the initialize members to
// `info` and `capabilities`. The ProtocolRouter in
// [github.com/ironpark/acp-go/router] serves both versions on one endpoint.
//
// Which methods are required is this package's call: the upstream v2 SDK
// registers handlers per method and enforces nothing. The split below keeps
// the same shape as v1 — the methods every agent or client needs to hold a
// conversation are required, everything gated by a capability is optional.
package acp2

import acp "github.com/ironpark/acp-go"

// ExtMethodHandler handles methods outside the spec; see [acp.ExtMethodHandler].
type ExtMethodHandler = acp.ExtMethodHandler

// ExtNotificationHandler handles notifications outside the spec; see
// [acp.ExtNotificationHandler].
type ExtNotificationHandler = acp.ExtNotificationHandler
