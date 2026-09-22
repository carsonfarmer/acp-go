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
- Outputs: `schema/{v1,v2}/schema.gen.go` (wire types), `schema/{v1,v2}/zod.gen.go` (Zod rule tables)
- Shared Zod evaluator: `schema/zod`

### SDK

- Typescript SDK `reference/typescript-sdk`
- Python SDK `reference/python-sdk`
- Rust Lib `reference/agent-client-protocol`
