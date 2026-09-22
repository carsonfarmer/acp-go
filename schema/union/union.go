// Package union recognizes the alternatives of the generated raw ACP unions
// (unions that are not discriminated objects). It is a runtime dependency of
// the generated schema packages rather than a public API; its shape may
// change with them.
package union

import (
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

// New encodes value as an alternative of the union. When the value's only
// rule requires literal members, they are spliced into the encoding so a
// zero-valued struct still produces its alternative.
func New[T any](union string, rules Rules, value T) (jsontext.Value, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	group := rules[reflect.TypeFor[T]()]
	if anyMatch(group, &payload{raw: raw}) {
		return raw, nil
	}
	if len(group) == 1 && len(group[0].Tags) > 0 {
		var fields map[string]jsontext.Value
		if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
			return nil, fmt.Errorf("%s: %T must encode as an object", union, value)
		}
		for _, tag := range group[0].Tags {
			fields[tag.Name] = tag.Value
		}
		if raw, err = json.Marshal(fields, json.Deterministic(true)); err != nil {
			return nil, err
		}
		if anyMatch(group, &payload{raw: raw}) {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("%s: %v is not a valid %T alternative", union, value, value)
}
