// Package union recognizes the alternatives of the generated raw ACP unions
// (unions that are not discriminated objects). It is a runtime dependency of
// the generated schema packages rather than a public API; its shape may
// change with them.
package union

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"reflect"

	"github.com/ironpark/go-acp/schema/zod"
)

// Rule describes when a JSON payload is one alternative of a raw union.
type Rule struct {
	Null     bool           // the payload must be JSON null
	NonNull  bool           // the payload must not be null or empty
	Required []string       // object members that must be present
	NotNull  []string       // object members that must not be null when present
	Tags     []Tag          // literal members the object must carry, sorted by name
	Literal  jsontext.Value // scalar literal the whole payload must equal
}

// Tag is an object member whose value must equal a literal.
type Tag struct {
	Name  string
	Value jsontext.Value
}

// Rules maps each alternative Go type of a union to the rules under which a
// payload is that alternative; a payload matches if any rule matches.
type Rules map[reflect.Type][]Rule

// Group is one entry of a Rules table, built with Alt.
type Group struct {
	Type  reflect.Type
	Rules []Rule
}

// Alt groups the rules under which a payload is a T.
func Alt[T any](rules ...Rule) Group {
	return Group{Type: reflect.TypeFor[T](), Rules: rules}
}

// Table builds a Rules table from its groups.
func Table(groups ...Group) Rules {
	rules := make(Rules, len(groups))
	for _, g := range groups {
		rules[g.Type] = g.Rules
	}
	return rules
}

// payload parses the object members of a JSON value at most once.
type payload struct {
	raw    jsontext.Value
	fields map[string]jsontext.Value
	parsed bool
}

func (p *payload) object() (map[string]jsontext.Value, bool) {
	if !p.parsed {
		p.parsed = true
		if json.Unmarshal(p.raw, &p.fields) != nil {
			p.fields = nil
		}
	}
	return p.fields, p.fields != nil
}

func (r *Rule) matches(p *payload) bool {
	raw := p.raw
	if r.Null && raw.Kind() != 'n' {
		return false
	}
	if r.NonNull && (raw.Kind() == 'n' || len(raw) == 0) {
		return false
	}
	if len(r.Literal) > 0 && !zod.Equal(raw, r.Literal) {
		return false
	}
	if len(r.Required) == 0 && len(r.NotNull) == 0 && len(r.Tags) == 0 {
		return true
	}
	fields, ok := p.object()
	if !ok {
		return false
	}
	for _, name := range r.Required {
		if _, ok := fields[name]; !ok {
			return false
		}
	}
	for _, name := range r.NotNull {
		if v, ok := fields[name]; ok && v.Kind() == 'n' {
			return false
		}
	}
	for _, tag := range r.Tags {
		if got, ok := fields[tag.Name]; !ok || !zod.Equal(got, tag.Value) {
			return false
		}
	}
	return true
}

func anyMatch(rules []Rule, p *payload) bool {
	for i := range rules {
		if rules[i].matches(p) {
			return true
		}
	}
	return false
}

// As decodes raw as T once one of T's rules in the union's table accepts it.
func As[T any](union string, rules Rules, raw jsontext.Value) (T, error) {
	var out T
	if !anyMatch(rules[reflect.TypeFor[T]()], &payload{raw: raw}) {
		return out, fmt.Errorf("%s: payload is not a %T", union, out)
	}
	if v, ok := any(&out).(*jsontext.Value); ok {
		*v = raw.Clone()
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("%s: decode %T: %w", union, out, err)
	}
	return out, nil
}

// New encodes value and checks the result is an alternative of the union.
// Object alternatives marshal their own literal members, so a zero-valued
// struct still produces its alternative.
func New[T any](union string, rules Rules, value T) (jsontext.Value, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if anyMatch(rules[reflect.TypeFor[T]()], &payload{raw: raw}) {
		return raw, nil
	}
	return nil, fmt.Errorf("%s: %v is not a valid %T alternative", union, value, value)
}

// SpliceTag writes payload as an object with the discriminator as its first member.
func SpliceTag(enc *jsontext.Encoder, tag, value string, payload any) error {
	raw, err := json.Marshal(payload, enc.Options())
	if err != nil {
		return err
	}
	if jsontext.Value(raw).Kind() != '{' {
		return fmt.Errorf("%s payload must be an object", tag)
	}
	out := append(make([]byte, 0, len(raw)+len(tag)+len(value)+6), '{')
	if out, err = jsontext.AppendQuote(out, tag); err != nil {
		return err
	}
	if out, err = jsontext.AppendQuote(append(out, ':'), value); err != nil {
		return err
	}
	if len(raw) > 2 {
		out = append(out, ',')
		out = append(out, raw[1:]...)
	} else {
		out = append(out, '}')
	}
	return enc.WriteValue(out)
}

// UnspliceTag checks the discriminator, removes it and decodes the rest into payload.
func UnspliceTag(dec *jsontext.Decoder, tag, value string, payload any) error {
	raw, err := dec.ReadValue()
	if err != nil {
		return err
	}
	if raw.Kind() != '{' {
		return fmt.Errorf("expected %s %q, got %s", tag, value, raw)
	}
	in := jsontext.NewDecoder(bytes.NewReader(raw), dec.Options())
	if _, err := in.ReadToken(); err != nil {
		return err
	}
	rest := make([]byte, 0, len(raw))
	rest = append(rest, '{')
	var got []byte // copied: the decoder reuses its buffer
	for in.PeekKind() != '}' {
		tok, err := in.ReadToken()
		if err != nil {
			return err
		}
		name := tok.String()
		member, err := in.ReadValue()
		if err != nil {
			return err
		}
		if name == tag {
			got = append(got[:0], member...) // last one wins, as with duplicate names allowed
			continue
		}
		if len(rest) > 1 {
			rest = append(rest, ',')
		}
		if rest, err = jsontext.AppendQuote(rest, name); err != nil {
			return err
		}
		rest = append(append(rest, ':'), member...)
	}
	var s string
	if err := json.Unmarshal(got, &s); err != nil || s != value {
		return fmt.Errorf("expected %s %q, got %s", tag, value, got)
	}
	return json.Unmarshal(append(rest, '}'), payload, dec.Options())
}
