# TypeScript schema generation (`next`)

The generator reads the official [TypeScript SDK](https://github.com/agentclientprotocol/typescript-sdk)
using [go-tree-sitter](https://github.com/tree-sitter/go-tree-sitter) and the TypeScript grammar.
`typescript/REVISION` pins the upstream commit; its license is in `typescript/LICENSE`.
Generation uses checked-in sources and requires no Node.js or network access.
Go 1.27 or newer is required by both modules. CGO and a C compiler are required for the
generator, but not for generated packages. Generated code uses `encoding/json/v2`,
`encoding/json/jsontext` and a generic decoder helper; no `GOEXPERIMENT` setting is needed.
Each version produces `schema.gen.go` (wire types) and `zod.gen.go` (Zod rule tables and
`Decode…JSON`/`Validate…JSON` functions). The rule evaluator lives once in `schema/zod` and is
shared by both versions; it is a runtime dependency of the generated packages, not a public API.

```sh
# From the repository root:
go generate ./...
go test ./...
(cd internal/cmd/schema && go test ./...)
(cd internal/cmd/schema && go run . -source ../../../schema/typescript -out ../../../schema -check)

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
The root `acp` package still uses its previous checked-in types. Connection/session runtime migration
is a separate stage; these new packages do not yet replace that runtime's API.
The previous JSON Schema generator, inputs and configuration have been removed.

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
still encode nil as null. These defaults apply to the new schema packages; the existing root
runtime still uses `encoding/json` pending its migration.

Literal unions produce named scalar types and constants. Other unions preserve their JSON payload
and expose generated constructors, `As…` decoding helpers, `Parse…` and `RawJSON`.
Streaming `MarshalJSONTo` / `UnmarshalJSONFrom` methods integrate with JSON v2 encoders and
decoders. Stored JSON is copied on decode and when returned to the caller, so decoding a copied
union value does not mutate the original.
Constructors set object discriminators and reject incorrect scalar literal values.
`As…` helpers check required properties, non-nullable object fields and literal tags
when the alternative is an object, then decode its Go representation. They are not substitutes for
the explicit Zod decoding API below. Unknown variants remain available through `RawJSON`
and round-trip unchanged when using the ordinary JSON decoder.

The generator first processes both versions before writing output, and `-check` detects stale
checked-in output without modifying it. Generator tests cover parsing failures, numeric hints,
compilation and JSON round trips of generated Go code, plus both pinned SDK versions.

## Generated API naming

Method constants use Go-style names such as `AgentMethodsSessionNew` and
`ClientMethodsSessionUpdate`. `CurrentProtocolVersion` is the numeric version constant;
`ProtocolVersion` is the wire type. Type names retain common initialisms, such as
`RequestID` and `MCPServerHTTP`. Collisions involving enum constants, constructors and
parse functions fail generation rather than producing Go code that cannot compile.

## Zod-aware decoding

Every SDK type has explicit `Decode<Type>JSON` and `Validate<Type>JSON` functions:

```go
request, err := acpv1.DecodeReadTextFileRequestJSON(data)
err = acpv2.ValidateRequestPermissionRequestJSON(data)
```

`Decode…JSON` validates and normalizes according to the supported Zod rules, then decodes
into the generated Go type. Rules are emitted as typed Go composite literals (`zod.Rule`) so
mistakes fail at compile time; regular expressions are compiled once at package init. `Validate…JSON` reports whether that same Zod parser accepts the
input, **including recovery/default behavior**; it is not a strict no-recovery validator.
Neither changes the behavior of ordinary `json.Unmarshal`, union `Parse…`, or `As…` methods.

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
