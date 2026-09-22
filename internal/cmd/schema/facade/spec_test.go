package facade

import (
	"strings"
	"testing"

	"github.com/ironpark/go-acp/internal/cmd/schema/tsdef"
)

const fixture = `
export type PingRequest = { sessionId: string; };
export type PingResponse = { ok: boolean; };
export type ByeNotification = { reason?: string; };
export type Marker = string;
export const AGENT_METHODS = { ping: "ping", session_bye: "session/bye", extra: "extra/one" } as const;
export const CLIENT_METHODS = { notice: "notice" } as const;
export const PROTOCOL_METHODS = { cancel_request: "$/cancel_request" } as const;
export const PROTOCOL_VERSION = 1;
`

func parse(t *testing.T) *tsdef.Schema {
	t.Helper()
	s, err := tsdef.Parse("fixture.ts", []byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func spec() *Spec {
	return &Spec{
		Package: "fixture", SchemaPath: "example.com/schema", Unhandled: []string{"extra/one"},
		ExtraTypes: []string{"Marker"},
		Agent: []Group{
			{Interface: "Agent", Required: true, Doc: "Agent doc.", Methods: []Method{
				{Wire: "ping", Name: "Ping", Params: "PingRequest", Response: "PingResponse", Doc: "Ping doc."},
			}},
			{Interface: "Byer", Methods: []Method{
				{Wire: "session/bye", Name: "Bye", Params: "ByeNotification", CallDoc: "Bye call."},
			}},
		},
		Client: []Group{
			{Interface: "Client", Required: true, Methods: []Method{
				{Wire: "notice", Name: "Notice", Params: "ByeNotification"},
			}},
		},
	}
}

func TestGenerateEmitsInterfacesCallsAndDispatch(t *testing.T) {
	files, err := Generate(spec(), parse(t))
	if err != nil {
		t.Fatal(err)
	}
	methods := string(files["methods.gen.go"])
	for _, want := range []string{
		"type Agent interface {",
		"Ping(ctx context.Context, params *PingRequest) (*PingResponse, error)",
		"type Byer interface {",
		"func (c *ClientSideConnection) Ping(",
		"acpconn.Call[PingResponse](ctx, c.conn, schema.AgentMethodsPing, params)",
		"func (c *ClientSideConnection) Bye(",
		"c.conn.SendNotification(ctx, schema.AgentMethodsSessionBye, params)",
		"case schema.AgentMethodsPing:\n\t\treturn acpconn.Request(ctx, schema.Validated, params, c.agent.Ping)",
		"if h, ok := c.agent.(Byer); ok {\n\t\t\treturn acpconn.Notify(ctx, schema.Validated, params, h.Bye)",
		"func (c *AgentSideConnection) handleRequest(",
		"func (c *ClientSideConnection) handleNotification(",
		"// Bye call.\nfunc (c *ClientSideConnection) Bye(",
	} {
		if !strings.Contains(methods, want) {
			t.Errorf("methods.gen.go lacks %q\n%s", want, methods)
		}
	}
	// gofmt aligns the alias block, so compare with whitespace collapsed.
	types := strings.Join(strings.Fields(string(files["types.gen.go"])), " ")
	for _, want := range []string{"PingRequest = schema.PingRequest", "ByeNotification = schema.ByeNotification", "Marker = schema.Marker", "const ProtocolVersion = schema.CurrentProtocolVersion"} {
		if !strings.Contains(types, want) {
			t.Errorf("types.gen.go lacks %q\n%s", want, types)
		}
	}
	// The extension method must not be routed and the protocol method never appears.
	for _, unwanted := range []string{"ExtraOne", "CancelRequest"} {
		if strings.Contains(methods, unwanted) {
			t.Errorf("methods.gen.go routes %s", unwanted)
		}
	}
}

func TestValidationRejectsDrift(t *testing.T) {
	cases := map[string]func(*Spec){
		"uncovered method constant": func(s *Spec) { s.Unhandled = nil },
		"unknown wire method":       func(s *Spec) { s.Agent[1].Methods[0].Wire = "session/gone" },
		"unknown params type":       func(s *Spec) { s.Agent[0].Methods[0].Params = "Nope" },
		"unknown response type":     func(s *Spec) { s.Agent[0].Methods[0].Response = "Nope" },
		"unknown extra type":        func(s *Spec) { s.ExtraTypes = []string{"Nope"} },
		"duplicate method kind": func(s *Spec) {
			s.Agent[1].Methods = append(s.Agent[1].Methods, s.Agent[1].Methods[0])
		},
		"duplicate Go name": func(s *Spec) {
			s.Agent[1].Methods = append(s.Agent[1].Methods, Method{Wire: "ping", Name: "Ping", Params: "ByeNotification"})
		},
		"no required group": func(s *Spec) { s.Agent[0].Required = false },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := spec()
			mutate(s)
			if _, err := Generate(s, parse(t)); err == nil {
				t.Fatal("generation accepted the drift")
			}
		})
	}
}

func TestSameWireMethodMayBeRequestAndNotification(t *testing.T) {
	s := spec()
	s.Agent[1].Methods = append(s.Agent[1].Methods, Method{Wire: "session/bye", Name: "ByeRequest", Params: "PingRequest", Response: "PingResponse"})
	if _, err := Generate(s, parse(t)); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedTablesMatchPinnedSchemas(t *testing.T) {
	for version, s := range map[string]*Spec{"v1": V1, "v2": V2} {
		schema, err := tsdef.ParseDir("../../../../schema/typescript/" + version)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Generate(s, schema); err != nil {
			t.Fatalf("%s: %v", version, err)
		}
	}
}
