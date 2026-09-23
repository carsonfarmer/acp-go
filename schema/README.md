# TypeScript schema generation (`next`)

The generator reads the official [TypeScript SDK](https://github.com/agentclientprotocol/typescript-sdk)
using [go-tree-sitter](https://github.com/tree-sitter/go-tree-sitter) and the TypeScript grammar.
`typescript/REVISION` pins the upstream commit; its license is in `typescript/LICENSE`.
Generation uses checked-in sources and requires no Node.js or network access.
Go 1.27 or newer is required by both modules. CGO and a C compiler are required for the
generator, but not for generated packages. Generated code uses `encoding/json/v2`,
`encoding/json/jsontext`; no `GOEXPERIMENT` setting is needed.
Each version produces the wire types split by kind — `methods.gen.go` (method constants and the
protocol version), `enums.gen.go` (identifier types and literal enums), `types.gen.go` (object
structs and aliases), `unions.gen.go` (tagged and raw payload unions) and
`envelope.gen.go` (the JSON-RPC envelope: `AgentRequest`, `ClientResponse`, `RequestID`, `Error` …) —
and `zod.gen.go` (Zod rule tables, the `Validated` option and generic `Decode`/`Validate`).
The split is by declaration kind, not by domain, so it needs no mapping table that could drift. The rule evaluator lives once in `schema/zod` and the
raw-union alternative matcher once in `schema/union`; both are shared by the versions as runtime
dependencies of the generated packages, not public APIs.

```sh
# From the repository root:
go generate ./...
go test ./...
(cd internal/cmd/schema && go test ./...)
(cd internal/cmd/schema && go run . -source ../../../schema/typescript -out ../../../schema -facade ../../.. -check)

# Refresh intentionally, using a full upstream commit SHA:
./schema/update.sh <40-character-commit-sha>
go generate ./...
```

Inputs and outputs:

| Upstream | Snapshot | Go import |
| --- | --- | --- |
| `src/schema/*.ts` | `typescript/v1` | `github.com/ironpark/go-acp/schema/v1` |
| `src/v2/schema/*.ts` | `typescript/v2` | `github.com/ironpark/go-acp/schema/v2` |

Both Go packages are named `schema`; use aliases such as `acpv1` and `acpv2` when importing both.
`acpv1` implements ACP v1 on top of `schema/v1`, `acpv2` implements the draft v2 on top of
`schema/v2`, the root `acp` package holds the runtime they share, and `router` serves both on one
endpoint. The previous JSON Schema generator,
inputs and configuration have been removed.

## Façade generation

`-facade <module root>` also writes `types.gen.go` and `methods.gen.go` into `acpv1` and `acpv2`. Their input is the method table in `internal/cmd/schema/facade/{v1,v2}.go`: how
wire methods group into Go interfaces, which are required, and what their docs say. The generator
checks the table against the schema constants and type names, so an upstream method that is
neither in the table nor listed as `Unhandled` fails generation instead of silently going unrouted.
The generated files hold the interfaces, the outgoing calls on both connection types, the dispatch
switches and the type aliases; the hand-written files keep the connection structs, constructors,
lifecycle methods and the extension hooks.

## Supported subset

- `types.gen.ts`: exported aliases, references, primitives, literal enums, objects,
  optional properties, nullable types, arrays, string index signatures, unions and object intersections.
- `index.ts`: protocol version and method constants. Imports, re-exports and private declarations
  are ignored. Unsupported exported declarations fail with a source location.
- `zod.gen.ts`: schema references, primitives, scalar literals, objects, records, arrays,
  unions/intersections, optional/nullable/nullish wrappers, defaults, integer/numeric/length
  bounds, regular expressions, URL and ISO datetime builders. The ACP helpers
  `defaultOnError`, `requiredDefaultOnError`, `vecSkipError`, `excludeKnownTags` and
  `preserveCustomPayload` retain their ordering and distinct semantics. Unsupported builder
  expressions fail with a source location; no JavaScript code is evaluated.
- `guards.gen.ts`: retained with the upstream snapshot for reference; not executed or translated.

Optional scalar fields use pointers with `omitzero`, retaining explicit false, zero and empty strings.
Optional slices and maps are plain values: nil is omitted and an empty non-nil value encodes as `[]` or `{}`.
Nullable fields use pointers; optional null and absence share the nil representation.
Literal unions that also admit the underlying primitive, such as `"a" | "b" | string`, produce a
named scalar type with constants, a `<Type>Values` list and a `Known` method.
Unconstrained TypeScript numbers use `float64`; unknown payloads use `jsontext.Value` to preserve
large numbers and extension data. Object index signatures use JSON v2's `embed` fallback:
additional properties retain their declared value type, and duplicate keys that collide with
named fields are rejected. Caller options such as deterministic map ordering propagate normally.

JSON v2 defaults reject duplicate object members and invalid UTF-8, and match field names
case-sensitively. Required nil slices/maps encode as empty arrays/objects. Nullable pointers
still encode nil as null. The root runtime uses `encoding/json/v2` as well, so these defaults
apply end to end.

Literal unions produce named scalar types and constants.

Discriminated object unions such as `SessionUpdate`, `ContentBlock` and `McpServer` become a small
wrapper struct around a sealed `<Type>Variant` interface. Each variant is a plain struct without
the discriminator member; the tag is implied by the Go type, written first by the variant's own
`MarshalJSONTo`, and checked by its `UnmarshalJSONFrom`. Decode with ordinary `json.Unmarshal`
and branch with a type switch:

```go
var update acpv2.SessionUpdate
if err := json.Unmarshal(data, &update); err != nil { ... }
switch v := update.Variant().(type) {
case acpv2.SessionUpdateAgentMessageChunk:
	// v.Content ...
case acpv2.SessionUpdateCustom:
	// v.SessionUpdate holds the unknown tag; v.AdditionalProperties keeps every member.
}
out := acpv2.NewSessionUpdate(acpv2.SessionUpdateAgentMessageChunk{Content: block})
```

A catch-all `{ tag: string; [key: string]: unknown }` member becomes `<Type>Custom`, so unknown
tags round-trip unchanged including large numbers. Members of the form `Inner & { tag: "x" }`
where `Inner` is itself a union (for example `StateUpdate` inside `SessionUpdate`) become
`struct { Value Inner }`; the outer tag is spliced into the inner object on encode and removed
on decode. Missing required members are not rejected by plain decoding; use `Validated` for
SDK-level validation. The zero wrapper encodes as `null`, `null` decodes to the zero wrapper, and
wrappers implement `IsZero`, so optional union fields are plain values omitted when unset.
Callers that prefer interface-typed fields can declare `<Type>Variant` fields directly and decode
with `json.WithUnmarshalers(acpv2.Unmarshalers)`; encoding needs no options.

Unions that are not discriminated objects (`RequestId`, `AgentResponse`, `ElicitationContentValue`,
method `params` unions, ...) preserve their JSON payload and expose a generic method `As[T]`, a
generic constructor `New<Union>[T]`, plus `Parse…` and `RawJSON`. Both are constrained by the
generated type set `<Union>Alternative`, so asking for a type the union cannot hold is a compile
error (generic methods require Go 1.27):

```go
req, err := msg.Params.As[acpv1.PromptRequest]()          // (T, error)
id, err := schemav1.NewRequestID("abc")                     // string | float64 | jsontext.Value
```

Alternatives that name the same Go type (`ExtResponse` and `MessageMCPResponse` are both
`jsontext.Value`) become one type-set term and one rule group; the generator resolves aliases through
the schema, so declaration order does not matter. Each alternative contributes a `union.Rule` —
required and non-nullable members, literal tags, scalar literal, null — kept in a per-union
`union.Table`; `As` decodes once any rule for `T` accepts the payload and otherwise reports why
not, and `New<Union>` rejects values that match no rule. Object types with required literal
members (`CreateElicitationRequestForm.Mode`) fix those members in their own `MarshalJSON` /
`MarshalJSONTo`, so a zero value encodes as its alternative with or without the constructor. Tagged unions offer the same `As[T]` over their `<Type>Variant` types
alongside the `Variant()` type switch. Streaming `MarshalJSONTo` / `UnmarshalJSONFrom` methods
integrate with JSON v2 encoders and decoders. Stored JSON is copied on decode and when returned
to the caller, so decoding a copied union value does not mutate the original.

The generator first processes both versions before writing output, and `-check` detects stale
checked-in output without modifying it. Generator tests cover parsing failures, numeric hints,
compilation and JSON round trips of generated Go code, plus both pinned SDK versions.

## Generated API naming

Method constants use Go-style names such as `AgentMethodsSessionNew` and
`ClientMethodsSessionUpdate`. `CurrentProtocolVersion` is the numeric version constant;
`ProtocolVersion` is the wire type. Type names retain common initialisms, such as
`RequestID` and `MCPServerHTTP`. Tagged-union variants are named `<Union><TagValue>`; when
the SDK already uses that name for the payload type (`AuthMethodTerminal`), the variant gets a
`Variant` suffix. Collisions involving enum constants, constructors and parse functions fail
generation rather than producing Go code that cannot compile.

## Zod-aware decoding

`Validated` is a `json.Options` value that applies the SDK's Zod rules to every generated
type met while unmarshaling, at any nesting depth. The generic `Decode` and `Validate`
functions do the same for one top-level value:

```go
var req acpv2.PromptRequest
err := json.Unmarshal(data, &req, acpv2.Validated)
req, err = acpv2.Decode[acpv2.PromptRequest](data)
err = acpv2.Validate[acpv2.RequestPermissionRequest](data)
```

`Decode` validates and normalizes according to the supported Zod rules, then decodes
into the generated Go type. `Validate` reports whether that same Zod parser accepts the
input, **including recovery/default behavior**; it is not a strict no-recovery validator.
Rules are emitted as typed Go composite literals (`zod.Rule`) so mistakes fail at compile time;
regular expressions are compiled once at package init. Plain `json.Unmarshal` without
`Validated`, union `Parse…` and `As[T]` stay lenient.

Identifier and other scalar SDK types are distinct Go types (`type SessionID string`), so they
carry their own rule and cannot be mixed up. Types that are Go aliases (`ExtRequest` and the other `jsontext.Value`
payloads, plus nullable scalars) share a `reflect.Type` with their
underlying type: `Validated` decodes them as that type and `Decode`/`Validate` reject them.

Missing input and JSON null remain distinct while applying rules. Defaults apply to missing
values; recovery may omit an invalid optional field or replace it with a literal fallback.
`requiredDefaultOnError` rejects a missing key while recovering a present invalid value.
`vecSkipError` removes invalid array items. Objects strip unknown keys, while custom union
variants restore unevaluated payload keys, preserving large JSON numbers. Reserved known tags
cannot use a custom catch-all to bypass a malformed known variant. Errors include schema and
field/index paths. Integer-valued JSON such as `1.0` is normalized before Go integer decoding.

This is the SDK's static Zod subset, not a JavaScript runtime or general Zod interpreter.
Arbitrary transforms/refinements, regex flags and unsupported options fail generation. Regexes
use Go's regexp engine; only expressions in the supported subset are accepted. URLs use Go's
absolute-URL parser, which is stricter than WHATWG for inputs such as `http:example.com`;
it is not a complete WHATWG implementation. ISO datetimes require seconds, validate dates and
respect the `offset` option. Input JSON rejects duplicate keys and invalid UTF-8. Schema
recursion is limited to 512 evaluator levels. Error wording is Go-specific.

Both pinned SDK versions are covered by Go tests and captured reference outcomes from
Zod 4.5.4; see [reference fixtures](testdata/README.md). The pinned SDK helper source is
included at `typescript/schema-deserialize.ts` to document the recovery and extension rules.
