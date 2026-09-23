// Package facade generates the typed connection façades for each protocol
// version from a method table plus the parsed schema. The table is the hand
// written half of the design: which wire methods exist is checked against the
// schema, but how they group into Go interfaces and what their docs say is
// decided here.
package facade

import (
	"fmt"
	"go/format"
	"slices"
	"strings"

	"github.com/ironpark/acp-go/internal/cmd/schema/tsdef"
	"github.com/ironpark/acp-go/internal/cmd/schema/tsgen"
)

// Spec describes one protocol version's façade package.
type Spec struct {
	// Package is the Go package name, Dir its directory relative to the façade
	// root, and SchemaPath the import path of the matching schema package.
	Package    string
	Dir        string
	SchemaPath string
	// Agent lists the methods an agent serves, grouped into interfaces; a client
	// connection gains an outgoing call for each. Client is the mirror image.
	Agent  []Group
	Client []Group
	// ExtraTypes are schema types re-exported in addition to every
	// Request/Response/Notification type, with an enum's constants and a
	// tagged union's variants and constructor.
	ExtraTypes []string
	// Unhandled are wire methods deliberately left to the extension handlers.
	Unhandled []string
}

// Group is one Go interface. The group whose Required flag is set becomes the
// Agent or Client interface itself; the others are optional interfaces the
// connection discovers by type assertion.
type Group struct {
	Interface string
	Required  bool
	Doc       string
	Methods   []Method
	// Experimental closes the interface's comment and each outgoing call's
	// with [tsgen.ExperimentalNote].
	Experimental bool
}

// stability appends the experimental note to s when the group is experimental.
func (g Group) stability(s string) string {
	if !g.Experimental || s == "" {
		return s
	}
	return s + "\n\n" + tsgen.ExperimentalNote
}

// Method is one wire method. A Method without a Response is a notification.
type Method struct {
	Wire     string
	Name     string
	Params   string
	Response string
	// Doc documents the handler on the interface; CallDoc documents the
	// outgoing call on the peer connection and defaults to Doc.
	Doc     string
	CallDoc string
	// Via names a hand-written method on the serving connection, with the
	// handler's signature, that the dispatch calls instead of the interface.
	// It lets the connection observe a method before the handler sees it.
	Via string
	// CallVia names a hand-written method on the calling connection, with the
	// outgoing call's signature, that the call goes through instead of
	// sending directly. It lets the connection fill in or observe the call.
	CallVia string
}

func (m Method) notification() bool { return m.Response == "" }

func (m Method) callDoc() string {
	if m.CallDoc != "" {
		return m.CallDoc
	}
	return m.Doc
}

// side is the per-direction naming the emitter needs.
type side struct {
	constants string // "AGENT_METHODS" or "CLIENT_METHODS"
	groups    []Group
	server    string // connection type serving these methods
	serverVar string // its handler field
	caller    string // connection type calling these methods on the peer
}

// Generate validates spec against schema and returns the façade files.
func Generate(spec *Spec, schema *tsdef.Schema) (map[string][]byte, error) {
	g := &emitter{spec: spec, types: map[string]bool{}, constants: map[string]string{}}
	for _, d := range schema.Types {
		g.types[tsgen.Name(d.Name)] = true
	}
	for _, c := range schema.Constants {
		if c.Name != "AGENT_METHODS" && c.Name != "CLIENT_METHODS" {
			continue
		}
		for _, m := range c.Members {
			wire := strings.Trim(m.Value, `"`)
			g.constants[c.Name+" "+wire] = tsgen.ConstantName(c.Name, m.Name)
		}
	}
	sides := []side{
		{"AGENT_METHODS", spec.Agent, "AgentSideConnection", "agent", "ClientSideConnection"},
		{"CLIENT_METHODS", spec.Client, "ClientSideConnection", "client", "AgentSideConnection"},
	}
	for _, s := range sides {
		if err := g.validate(s); err != nil {
			return nil, err
		}
	}

	decls, err := tsgen.Declarations(schema)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	g.typesFile(schema, decls)
	if err := g.flush(files, "types.gen.go"); err != nil {
		return nil, err
	}
	g.methodsFile(sides)
	if err := g.flush(files, "methods.gen.go"); err != nil {
		return nil, err
	}
	return files, nil
}

type emitter struct {
	spec      *Spec
	types     map[string]bool
	constants map[string]string // "AGENT_METHODS session/load" -> Go constant
	out       strings.Builder
}

func (g *emitter) write(f string, a ...any) { fmt.Fprintf(&g.out, f, a...) }

func (g *emitter) flush(files map[string][]byte, name string) error {
	src, err := format.Source([]byte(g.out.String()))
	if err != nil {
		return fmt.Errorf("format %s: %w\n%s", name, err, g.out.String())
	}
	files[name] = src
	g.out.Reset()
	return nil
}

// validate checks the table against the schema so that an upstream change
// fails generation instead of silently leaving a method unrouted.
func (g *emitter) validate(s side) error {
	covered := map[string]bool{}
	names := map[string]bool{}
	required := 0
	for _, group := range s.groups {
		if group.Required {
			required++
		}
		for _, m := range group.Methods {
			kind := "request"
			if m.notification() {
				kind = "notification"
			}
			key := m.Wire + " " + kind
			if covered[key] {
				return fmt.Errorf("%s: %s listed twice as a %s", s.constants, m.Wire, kind)
			}
			covered[key] = true
			if names[m.Name] {
				return fmt.Errorf("%s: Go method %s used twice", s.constants, m.Name)
			}
			names[m.Name] = true
			if _, ok := g.constants[s.constants+" "+m.Wire]; !ok {
				return fmt.Errorf("%s: %s is not a method constant in this schema", s.constants, m.Wire)
			}
			if !g.types[m.Params] {
				return fmt.Errorf("%s: params type %s not in schema", m.Wire, m.Params)
			}
			if !m.notification() && !g.types[m.Response] {
				return fmt.Errorf("%s: response type %s not in schema", m.Wire, m.Response)
			}
		}
	}
	if required != 1 {
		return fmt.Errorf("%s: expected exactly one required group, found %d", s.constants, required)
	}
	unhandled := map[string]bool{}
	for _, wire := range g.spec.Unhandled {
		unhandled[wire] = true
	}
	for key := range g.constants {
		prefix, wire, _ := strings.Cut(key, " ")
		if prefix != s.constants {
			continue
		}
		if !covered[wire+" request"] && !covered[wire+" notification"] && !unhandled[wire] {
			return fmt.Errorf("%s: %s is neither in the method table nor listed as unhandled", s.constants, wire)
		}
	}
	for _, t := range g.spec.ExtraTypes {
		if !g.types[t] {
			return fmt.Errorf("extra type %s not in schema", t)
		}
	}
	return nil
}

func (g *emitter) header() {
	g.write("// Code generated by acp-schema from the method table; DO NOT EDIT.\n\npackage %s\n\n", g.spec.Package)
}

// typesFile re-exports the payload types so a caller implementing the
// interfaces needs one import.
func (g *emitter) typesFile(schema *tsdef.Schema, decls map[string]tsgen.Decl) {
	g.header()
	g.write("import schema %q\n\n", g.spec.SchemaPath)
	g.write("// ProtocolVersion is the ACP protocol version implemented by this package.\nconst ProtocolVersion = schema.CurrentProtocolVersion\n\n")
	g.write("// Every request, response and notification payload, re-exported so that a\n// caller implementing [Agent] or [Client] needs one import. Other generated\n// types live in the schema package.\n\ntype (\n")
	for _, d := range schema.Types {
		name := tsgen.Name(d.Name)
		if !payloadType(name) {
			continue
		}
		g.write("\t%s = schema.%s\n", name, name)
	}
	g.write(")\n\n// Identifiers and values shared across those payloads, with the constants\n// and variants that come with them.\n\ntype (\n")
	var constants, constructors []string
	for _, t := range g.spec.ExtraTypes {
		g.write("\t%s = schema.%s\n", t, t)
		d := decls[t]
		constants = append(constants, d.Constants...)
		if d.Constructor == "" {
			continue
		}
		constructors = append(constructors, t)
		g.write("\t%s = schema.%s\n", d.Interface, d.Interface)
		for _, v := range d.Variants {
			if !payloadType(v) && !slices.Contains(g.spec.ExtraTypes, v) {
				g.write("\t%s = schema.%s\n", v, v)
			}
		}
	}
	// Meta is declared by the generator rather than the TypeScript schema, for
	// the _meta member every protocol version reserves.
	g.write("\tMeta = schema.Meta\n")
	g.write(")\n")
	if len(constants) > 0 {
		g.write("\nconst (\n")
		for _, c := range constants {
			g.write("\t%s = schema.%s\n", c, c)
		}
		g.write(")\n")
	}
	for _, t := range constructors {
		d := decls[t]
		g.write("\n// %s wraps a variant; a nil variant yields the zero value.\n", d.Constructor)
		g.write("func %s(v %s) %s { return schema.%s(v) }\n", d.Constructor, d.Interface, t, d.Constructor)
	}
}

// payloadType reports whether a schema type is a method payload rather than
// one of the JSON-RPC envelope unions.
func payloadType(name string) bool {
	if slices.Contains([]string{"AgentRequest", "AgentResponse", "AgentNotification", "ClientRequest", "ClientResponse", "ClientNotification", "ProtocolLevelNotification"}, name) {
		return false
	}
	for _, suffix := range []string{"Request", "Response", "Notification"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func doc(s string) string {
	if s == "" {
		return ""
	}
	return "// " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n// ") + "\n"
}

// methodsFile emits the interfaces, the outgoing calls and the dispatch
// switches for both directions.
func (g *emitter) methodsFile(sides []side) {
	g.header()
	g.write("// The hand-written half of each façade provides AgentSideConnection and\n// ClientSideConnection with a conn *jsonrpc.Connection field and an agent or\n// client field holding the served interface; the lifecycle and extension\n// methods every connection shares, and everything routed by method name, are\n// generated here.\n\n")
	g.write("import (\n\t\"context\"\n\t\"encoding/json/jsontext\"\n\n\tacp \"github.com/ironpark/acp-go\"\n\t\"github.com/ironpark/acp-go/internal/acpconn\"\n\t\"github.com/ironpark/acp-go/internal/jsonrpc\"\n\tschema %q\n)\n\n", g.spec.SchemaPath)
	for _, s := range sides {
		g.interfaces(s)
	}
	for _, s := range sides {
		g.connection(s.server)
	}
	for _, s := range sides {
		g.outgoing(s)
	}
	for _, s := range sides {
		g.dispatch(s)
	}
}

// connection emits the [acp.Conn] methods, which every connection type
// forwards to its JSON-RPC connection alike.
func (g *emitter) connection(typ string) {
	g.write("var _ acp.Conn = (*%s)(nil)\n\n", typ)
	g.write("// Start processes messages until the peer disconnects or ctx is cancelled.\nfunc (c *%s) Start(ctx context.Context) error { return c.conn.Start(ctx) }\n\n", typ)
	g.write("// Close shuts the connection down, waiting for in-flight handlers.\nfunc (c *%s) Close() error { return c.conn.Close() }\n\n", typ)
	g.write("// Done is closed once the connection stops.\nfunc (c *%s) Done() <-chan struct{} { return c.conn.Done() }\n\n", typ)
	g.write("// ExtMethod sends a request outside the spec and returns its raw result.\nfunc (c *%s) ExtMethod(ctx context.Context, method string, params any) (jsontext.Value, error) {\n\treturn c.conn.SendRequest(ctx, method, params)\n}\n\n", typ)
	g.write("// ExtNotification sends a notification outside the spec.\nfunc (c *%s) ExtNotification(ctx context.Context, method string, params any) error {\n\treturn c.conn.SendNotification(ctx, method, params)\n}\n\n", typ)
}

func (g *emitter) interfaces(s side) {
	for _, group := range s.groups {
		g.write("%stype %s interface {\n", doc(group.stability(group.Doc)), group.Interface)
		for i, m := range group.Methods {
			if i > 0 {
				g.write("\n")
			}
			g.write("%s\t%s\n", indentDoc(m.Doc), signature(m))
		}
		g.write("}\n\n")
	}
}

func indentDoc(s string) string {
	if s == "" {
		return ""
	}
	return "\t" + strings.ReplaceAll(doc(s), "\n// ", "\n\t// ")
}

func signature(m Method) string {
	if m.notification() {
		return fmt.Sprintf("%s(ctx context.Context, params *%s) error", m.Name, m.Params)
	}
	return fmt.Sprintf("%s(ctx context.Context, params *%s) (*%s, error)", m.Name, m.Params, m.Response)
}

// outgoing emits the caller connection's methods for reaching the peer.
func (g *emitter) outgoing(s side) {
	g.write("// --- Outgoing calls from %s to the peer ---\n\n", s.caller)
	for _, group := range s.groups {
		for _, m := range group.Methods {
			constant := g.constants[s.constants+" "+m.Wire]
			g.write("%sfunc (c *%s) %s {\n", doc(group.stability(m.callDoc())), s.caller, signature(m))
			switch {
			case m.CallVia != "":
				g.write("\treturn c.%s(ctx, params)\n}\n\n", m.CallVia)
			case m.notification():
				g.write("\treturn c.conn.SendNotification(ctx, schema.%s, params)\n}\n\n", constant)
			default:
				g.write("\treturn acpconn.Call[%s](ctx, c.conn, schema.%s, params)\n}\n\n", m.Response, constant)
			}
		}
	}
}

// dispatch emits the server connection's request and notification routing.
func (g *emitter) dispatch(s side) {
	for _, notification := range []bool{false, true} {
		if notification {
			g.write("func (c *%s) handleNotification(ctx context.Context, method string, params jsontext.Value) error {\n", s.server)
		} else {
			g.write("func (c *%s) handleRequest(ctx context.Context, method string, params jsontext.Value) (any, error) {\n", s.server)
		}
		g.write("\tswitch method {\n")
		for _, group := range s.groups {
			for _, m := range group.Methods {
				if m.notification() != notification {
					continue
				}
				constant := g.constants[s.constants+" "+m.Wire]
				helper, ret := "acpconn.Notify", "return "
				if !notification {
					helper = "acpconn.Request"
				}
				g.write("\tcase schema.%s:\n", constant)
				if m.Via != "" {
					g.write("\t\t%s%s(ctx, schema.Validated, params, c.%s)\n", ret, helper, m.Via)
				} else if group.Required {
					g.write("\t\t%s%s(ctx, schema.Validated, params, c.%s.%s)\n", ret, helper, s.serverVar, m.Name)
				} else {
					g.write("\t\tif h, ok := c.%s.(%s); ok {\n\t\t\t%s%s(ctx, schema.Validated, params, h.%s)\n\t\t}\n", s.serverVar, group.Interface, ret, helper, m.Name)
				}
			}
		}
		if notification {
			g.write("\tdefault:\n\t\tif h, ok := c.%s.(ExtNotificationHandler); ok {\n\t\t\treturn h.ExtNotification(ctx, method, params)\n\t\t}\n\t}\n\treturn jsonrpc.MethodNotFound(method)\n}\n\n", s.serverVar)
		} else {
			g.write("\tdefault:\n\t\tif h, ok := c.%s.(ExtMethodHandler); ok {\n\t\t\treturn h.ExtMethod(ctx, method, params)\n\t\t}\n\t}\n\treturn nil, jsonrpc.MethodNotFound(method)\n}\n\n", s.serverVar)
		}
	}
}
