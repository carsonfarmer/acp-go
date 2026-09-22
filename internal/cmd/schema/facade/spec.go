// Package facade generates the typed connection façades for each protocol
// version from a method table plus the parsed schema. The table is the hand
// written half of the design: which wire methods exist is checked against the
// schema, but how they group into Go interfaces and what their docs say is
// decided here.
package facade

import (
	"fmt"
	"go/format"
	"strings"

	"github.com/ironpark/go-acp/internal/cmd/schema/tsdef"
	"github.com/ironpark/go-acp/internal/cmd/schema/tsgen"
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
	// Request/Response/Notification type.
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
			g.constants[c.Name+" "+wire] = tsgen.Name(c.Name) + tsgen.Name(m.Name)
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

	files := map[string][]byte{}
	g.typesFile(schema)
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
func (g *emitter) typesFile(schema *tsdef.Schema) {
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
	g.write(")\n\n// Identifiers and values shared across those payloads.\n\ntype (\n")
	for _, t := range g.spec.ExtraTypes {
		g.write("\t%s = schema.%s\n", t, t)
	}
	g.write(")\n")
}

// payloadType reports whether a schema type is a method payload rather than
// one of the JSON-RPC envelope unions.
func payloadType(name string) bool {
	for _, envelope := range []string{"AgentRequest", "AgentResponse", "AgentNotification", "ClientRequest", "ClientResponse", "ClientNotification", "ProtocolLevelNotification"} {
		if name == envelope {
			return false
		}
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
	g.write("// The hand-written half of each façade provides AgentSideConnection and\n// ClientSideConnection with a conn *jsonrpc.Connection field and an agent or\n// client field holding the served interface; everything routed by method name\n// is generated here.\n\n")
	g.write("import (\n\t\"context\"\n\t\"encoding/json/jsontext\"\n\n\t\"github.com/ironpark/go-acp/internal/acpconn\"\n\t\"github.com/ironpark/go-acp/internal/jsonrpc\"\n\tschema %q\n)\n\n", g.spec.SchemaPath)
	for _, s := range sides {
		g.interfaces(s)
	}
	for _, s := range sides {
		g.outgoing(s)
	}
	for _, s := range sides {
		g.dispatch(s)
	}
}

func (g *emitter) interfaces(s side) {
	for _, group := range s.groups {
		g.write("%stype %s interface {\n", doc(group.Doc), group.Interface)
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
			g.write("%sfunc (c *%s) %s {\n", doc(m.callDoc()), s.caller, signature(m))
			if m.notification() {
				g.write("\treturn c.conn.SendNotification(ctx, schema.%s, params)\n}\n\n", constant)
			} else {
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
				if group.Required {
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
