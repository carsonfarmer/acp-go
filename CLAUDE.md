ACP(Agent Client Protocol) for golang

## reference for implement

### Docs

- What is ACP `reference/agent-client-protocol/docs/get-started/introduction.mdx`
- Protocol Details `reference/agent-client-protocol/docs/protocol`

### Go baseline (`next`)

- Both modules require Go 1.27 or newer.
- New schema code uses `encoding/json/v2` and `encoding/json/jsontext`.
- Use generics for reusable typed operations; do not maintain pre-generics compatibility.
- Optional pointer fields use `omitzero` so explicit empty values survive JSON v2 encoding.

### Schema generation (`next`)

- Inputs: `schema/typescript/v1/*.ts`, `schema/typescript/v2/*.ts`
- Upstream revision: `schema/typescript/REVISION`
- Generator: `internal/cmd/schema` (separate Go module)
- Details: `schema/README.md`
- Outputs: `schema/{v1,v2}/schema.gen.go` (wire types), `schema/{v1,v2}/zod.gen.go` (Zod rule tables),
  plus `types.gen.go`/`methods.gen.go` in the root and `acpv2` façade packages (from `internal/cmd/schema/facade/{v1,v2}.go`)
- Adding or regrouping a protocol method: edit the façade table, run `go generate ./...`; never edit `*.gen.go`
- Shared Zod evaluator: `schema/zod`

### Packages (`next`)

- Root `acp`: ACP v1 façade on `schema/v1`. `acpv2`: draft v2 façade on `schema/v2` (never imports root).
- `router`: `ProtocolRouter` serving both versions on one endpoint (imports both façades).
- `internal/jsonrpc`: version-agnostic JSON-RPC 2.0 core. `internal/acpconn`: shared options and generic dispatch.

### SDK

- Typescript SDK `reference/typescript-sdk`
- Python SDK `reference/python-sdk`
- Rust Lib `reference/agent-client-protocol`
