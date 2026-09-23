// Package tsgen generates Go wire types from the ACP TypeScript schema subset.
package tsgen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/ironpark/go-acp/internal/cmd/schema/tsdef"
)

type generator struct {
	defs         map[string]*tsdef.Type
	pending      []tsdef.Definition
	names        map[string]bool
	aliases      map[string]bool // Go names declared with "type X = ..."
	unmarshalers []string        // json.UnmarshalFromFunc entries for variant interfaces
	pkg          string
	buffers      map[string]*bytes.Buffer // output file name -> source being built
	order        []string                 // buffer creation order, for deterministic output
	out          *bytes.Buffer            // buffer the next write goes to
}

// Output file names. Declarations are split by kind so that the payload
// structs a caller reads are not interleaved with the JSON-RPC envelope
// unions the SDK uses internally.
const (
	fileMethods  = "methods.gen.go"  // method constants and the protocol version
	fileEnums    = "enums.gen.go"    // scalar identifier types and literal enums
	fileTypes    = "types.gen.go"    // object structs and aliases
	fileUnions   = "unions.gen.go"   // tagged and raw payload unions, plus the shared helpers
	fileEnvelope = "envelope.gen.go" // JSON-RPC envelope types: requests, responses, ids, errors
	fileZod      = "zod.gen.go"      // Zod rule tables and Validated/Decode/Validate
)

// envelope reports whether a type belongs to the JSON-RPC envelope rather than
// to a method payload.
func envelope(name string) bool {
	for _, prefix := range []string{"AgentRequest", "AgentResponse", "AgentNotification", "ClientRequest", "ClientResponse", "ClientNotification", "ProtocolLevelNotification", "RequestID"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return name == "Error"
}

// use directs subsequent writes to the named output file, starting it with
// its header on first use.
func (g *generator) use(name string) {
	if buf, ok := g.buffers[name]; ok {
		g.out = buf
		return
	}
	buf := &bytes.Buffer{}
	g.buffers[name] = buf
	g.order = append(g.order, name)
	g.out = buf
}

// Files maps generated file names to their formatted Go source.
type Files map[string][]byte

// newGenerator registers every definition under its Go name so references
// resolve before any declaration is emitted.
func newGenerator(schema *tsdef.Schema, pkg string) (*generator, error) {
	g := &generator{defs: map[string]*tsdef.Type{}, names: map[string]bool{}, aliases: map[string]bool{}, pkg: pkg, buffers: map[string]*bytes.Buffer{}}
	for _, d := range schema.Types {
		name := Name(d.Name)
		if g.names[name] {
			return nil, fmt.Errorf("duplicate Go type %s", name)
		}
		g.names[name] = true
		g.defs[d.Name] = d.Type
		d.Name = name
		g.pending = append(g.pending, d)
	}
	return g, nil
}

// Generate emits deterministic Go declarations. Unknown JSON payloads are kept
// as jsontext.Value, so custom/future variants survive a decode/encode cycle.
// Wire types are split by kind into methods, enums, types, unions and envelope
// files; Zod rules and Decode/Validate functions, when the schema has
// validators, go to zod.gen.go.
func Generate(schema *tsdef.Schema, pkg string) (Files, error) {
	if !token.IsIdentifier(pkg) || token.Lookup(pkg).IsKeyword() || pkg == "_" {
		return nil, fmt.Errorf("invalid package name %q", pkg)
	}
	g, err := newGenerator(schema, pkg)
	if err != nil {
		return nil, err
	}
	g.use(fileMethods)
	for _, c := range schema.Constants {
		if len(c.Members) > 0 {
			for _, m := range c.Members {
				name := Name(c.Name) + Name(m.Name)
				if g.names[name] {
					return nil, fmt.Errorf("duplicate Go declaration %s", name)
				}
				g.names[name] = true
				g.write("const %s = %s\n", name, m.Value)
			}
		} else {
			name := Name(c.Name)
			if c.Name == "PROTOCOL_VERSION" {
				name = "CurrentProtocolVersion"
			}
			if g.names[name] {
				return nil, fmt.Errorf("duplicate Go declaration %s", name)
			}
			g.names[name] = true
			g.write("const %s = %s\n", name, c.Value)
		}
	}
	for i := 0; i < len(g.pending); i++ {
		if err := g.definition(g.pending[i]); err != nil {
			return nil, fmt.Errorf("%s: %w", g.pending[i].Name, err)
		}
	}
	if len(g.unmarshalers) > 0 {
		g.use(fileUnions)
		g.write("\n// Unmarshalers decodes the tagged-union variant interfaces directly, for callers\n// that declare fields of those interface types instead of the wrapper structs:\n//\n//\tjson.Unmarshal(data, &v, json.WithUnmarshalers(schema.Unmarshalers))\nvar Unmarshalers = json.JoinUnmarshalers(\n%s,\n)\n", strings.Join(g.unmarshalers, ",\n"))
	}
	g.use(fileZod)
	if err := g.zod(schema, pkg); err != nil {
		return nil, err
	}
	files := Files{}
	for _, name := range g.order {
		if err := g.flush(files, name); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// flush formats the buffered source into files[name] and resets the buffer.
func (g *generator) flush(files Files, name string) error {
	buf := g.buffers[name]
	if buf.Len() == 0 {
		return nil
	}
	imports, err := usedImports(buf.Bytes())
	if err != nil {
		return fmt.Errorf("scan %s: %w", name, err)
	}
	var src bytes.Buffer
	fmt.Fprintf(&src, "// Code generated by acp-schema from the TypeScript SDK; DO NOT EDIT.\n\npackage %s\n\n", g.pkg)
	if len(imports) > 0 {
		src.WriteString("import (\n")
		for i, path := range imports {
			if i > 0 && strings.Contains(path, ".") != strings.Contains(imports[i-1], ".") {
				src.WriteString("\n") // standard library first, then module packages
			}
			fmt.Fprintf(&src, "%q\n", path)
		}
		src.WriteString(")\n\n")
	}
	src.Write(buf.Bytes())
	result, err := format.Source(src.Bytes())
	if err != nil {
		return fmt.Errorf("format %s: %w", name, err)
	}
	files[name] = result
	buf.Reset()
	return nil
}

func (g *generator) write(f string, a ...any) { fmt.Fprintf(g.out, f, a...) }

// importPaths maps the package names generated code may reference to their
// import paths; flush imports exactly those a file uses.
var importPaths = map[string]string{
	"json": "encoding/json/v2", "jsontext": "encoding/json/jsontext", "fmt": "fmt",
	"reflect": "reflect", "regexp": "regexp", "slices": "slices",
	"union": UnionRuntime, "zod": ZodRuntime,
}

// usedImports parses a body of declarations and returns the sorted import
// paths of the packages it selects from.
func usedImports(body []byte) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "", append([]byte("package p\n"), body...), parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				if path, ok := importPaths[id.Name]; ok {
					used[path] = true
				}
			}
		}
		return true
	})
	return slices.Sorted(maps.Keys(used)), nil
}

func comment(s string) string {
	if s == "" {
		return ""
	}
	return "// " + strings.ReplaceAll(s, "\n", "\n// ") + "\n"
}

// Name maps SDK identifiers to exported Go identifiers using common initialisms.
func Name(s string) string {
	var words []string
	var word []rune
	rs := []rune(s)
	flush := func() {
		if len(word) > 0 {
			words = append(words, string(word))
			word = nil
		}
	}
	for i, r := range rs {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(word) > 0 && unicode.IsUpper(r) && (unicode.IsLower(rs[i-1]) || (i+1 < len(rs) && unicode.IsLower(rs[i+1]) && unicode.IsUpper(rs[i-1]))) {
			flush()
		}
		word = append(word, r)
	}
	flush()
	for i, w := range words {
		lower := strings.ToLower(w)
		switch lower {
		case "id", "rpc", "url", "uri", "http", "https", "json", "api", "mcp", "acp":
			words[i] = strings.ToUpper(lower)
		default:
			runes := []rune(lower)
			runes[0] = unicode.ToUpper(runes[0])
			words[i] = string(runes)
		}
	}
	result := strings.Join(words, "")
	if result == "" {
		return "Empty"
	}
	if unicode.IsDigit([]rune(result)[0]) {
		result = "Value" + result
	}
	return result
}
