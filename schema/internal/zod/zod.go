// Package zod evaluates the statically extracted Zod rules used by the
// generated ACP schema packages. It is a runtime dependency of those packages
// rather than a general Zod implementation, internal so that it can change
// with them.
package zod

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"regexp"
	"slices"
)

// Kind identifies a supported Zod builder or ACP helper.
type Kind uint8

const (
	KindInvalid Kind = iota
	KindRef
	KindOptional
	KindNullish
	KindNullable
	KindDefault
	KindCatch
	KindRequiredCatch
	KindUnknown
	KindAny
	KindNever
	KindUnion
	KindIntersection
	KindExcludeTags
	KindPreserve
	KindOpenTags
	KindMin
	KindMax
	KindGte
	KindLte
	KindRegex
	KindInt
	KindNull
	KindString
	KindBoolean
	KindNumber
	KindLiteral
	KindURL
	KindDateTime
	KindArray
	KindSkipArray
	KindObject
	KindRecord
)

// Rule is one node of a statically parsed Zod schema. Value is JSON for
// literals, defaults, catch fallbacks and bounds; Ref names another registry entry.
type Rule struct {
	Kind    Kind
	Ref     string
	Inner   *Rule
	Members []*Rule
	Fields  []Field
	Key     *Rule
	Value   jsontext.Value
	Tag     string
	Tags    []string
	Regexp  *regexp.Regexp
	Offset  bool

	// Set by Link; a rule that was never linked is evaluated without them.
	linked   bool
	target   *Rule            // KindRef: the rule Ref names
	bound    float64          // KindMin, KindMax, KindGte, KindLte: Value as a number
	lit      jsontext.Value   // KindLiteral: Value in canonical form
	flat     *Rule            // KindIntersection: the members as one object
	tag      string           // KindUnion: the property whose value picks the member
	cases    map[string]*Rule // KindUnion: the member for each tag value
	fallback *Rule            // KindUnion: the member for any other value, if any
	value    *node            // KindDefault, KindCatch, KindRequiredCatch: Value as a tree
	// needsValue reports that the rule rejects an absent value, so an
	// optional or recovering rule around it need not try it on one.
	needsValue bool
}

// Field is a named object property.
type Field struct {
	Name   string
	Schema *Rule
}

// Registry maps schema names to their top-level rules.
type Registry map[string]*Rule

// Link prepares r's rules for evaluation and returns r. The generated
// packages call it once, when they initialize; evaluation reads the prepared
// rules concurrently, so they are not changed afterwards. It resolves each
// reference, reads each bound and literal once, merges an intersection of
// objects with distinct properties into one object, and indexes a union
// whose members are objects told apart by a string literal property, so
// evaluation picks that member instead of trying each in turn. None of these
// changes what a rule accepts or produces.
func Link(r Registry) Registry {
	linked := map[*Rule]bool{}
	var walk func(*Rule)
	walk = func(s *Rule) {
		if s == nil || linked[s] {
			return
		}
		linked[s] = true
		walk(s.Inner)
		walk(s.Key)
		for _, m := range s.Members {
			walk(m)
		}
		for _, f := range s.Fields {
			walk(f.Schema)
		}
		r.link(s)
	}
	for _, name := range slices.Sorted(maps.Keys(r)) {
		walk(r[name])
	}
	// Intersections and unions look through references to rules that may be
	// linked after them, so they are prepared once every rule is; an
	// intersection is merged when first looked through.
	l := &linker{r: r, flattened: map[*Rule]bool{}}
	decided := map[*Rule]bool{}
	for s := range linked {
		s.needsValue = r.needsValue(s, decided)
	}
	for s := range linked {
		switch s.Kind {
		case KindIntersection:
			l.flatten(s)
		case KindUnion:
			l.index(s)
		}
	}
	return r
}

// needsValue reports whether s rejects an absent value whatever it wraps.
// It errs towards false, which only costs an evaluation: a rule reached
// again through a reference while it is being decided counts as accepting.
// decided holds each rule's answer, false until it is known.
func (r Registry) needsValue(s *Rule, decided map[*Rule]bool) bool {
	if s == nil {
		return false
	}
	if answer, ok := decided[s]; ok {
		return answer
	}
	decided[s] = false
	answer := r.decideNeedsValue(s, decided)
	decided[s] = answer
	return answer
}

func (r Registry) decideNeedsValue(s *Rule, visiting map[*Rule]bool) bool {
	switch s.Kind {
	case KindNull, KindString, KindBoolean, KindNumber, KindLiteral, KindURL, KindDateTime,
		KindArray, KindSkipArray, KindObject, KindRecord, KindNever, KindRequiredCatch:
		return true
	case KindInt:
		return s.Inner == nil || r.needsValue(s.Inner, visiting)
	case KindRef:
		return r.needsValue(r[s.Ref], visiting)
	case KindNullable, KindMin, KindMax, KindGte, KindLte, KindRegex, KindOpenTags, KindExcludeTags, KindPreserve:
		return r.needsValue(s.Inner, visiting)
	case KindUnion:
		for _, m := range s.Members {
			if !r.needsValue(m, visiting) {
				return false
			}
		}
		return len(s.Members) > 0
	case KindIntersection:
		for _, m := range s.Members {
			if r.needsValue(m, visiting) {
				return true
			}
		}
	}
	return false
}

// linker merges intersections and indexes unions for Link.
type linker struct {
	r Registry
	// flattened holds the intersections already considered, including one
	// being merged, so an intersection that reaches itself is left as is.
	flattened map[*Rule]bool
}

func (r Registry) link(s *Rule) {
	s.linked = true
	switch s.Kind {
	case KindRef:
		s.target = r[s.Ref]
	case KindMin, KindMax, KindGte, KindLte:
		if json.Unmarshal(s.Value, &s.bound) != nil {
			s.linked = false // evaluation reports the invalid bound
		}
	case KindLiteral:
		s.lit = s.Value.Clone()
		if s.lit.Canonicalize() != nil {
			s.lit = nil
		}
	case KindDefault, KindCatch, KindRequiredCatch:
		if s.Value.IsValid() {
			s.value = parseTree(s.Value)
		}
	}
}

// object returns the object rule s is, looking through references and
// merged intersections, or nil.
func (l *linker) object(s *Rule) *Rule {
	for range 64 {
		switch {
		case s == nil:
			return nil
		case s.Kind == KindRef:
			s = l.r[s.Ref]
		case s.Kind == KindIntersection:
			l.flatten(s)
			if s.flat == nil {
				return nil
			}
			s = s.flat
		case s.Kind == KindObject:
			return s
		default:
			return nil
		}
	}
	return nil
}

// flatten merges an intersection whose members are all objects with distinct
// properties into one object with every member's properties. Evaluating it
// accepts and produces what evaluating the members and merging their results
// does, without the merge.
func (l *linker) flatten(s *Rule) {
	if l.flattened[s] {
		return
	}
	l.flattened[s] = true
	flat := &Rule{Kind: KindObject, linked: true}
	names := map[string]bool{}
	for _, m := range s.Members {
		o := l.object(m)
		if o == nil {
			return
		}
		for _, f := range o.Fields {
			if names[f.Name] {
				return
			}
			names[f.Name] = true
			flat.Fields = append(flat.Fields, f)
		}
	}
	s.flat = flat
}

// index finds the property that tells a union's members apart: one every
// member requires to be a string literal, with a different literal in each.
// One member may instead be the custom payload that [KindExcludeTags] keeps
// from every one of those literals. Only the member with the literal a value
// carries can accept it, and only the custom payload one without, so trying
// that member alone decides the union.
func (l *linker) index(s *Rule) {
	if len(s.Members) < 2 {
		return
	}
	for _, name := range l.literalNames(s.Members) {
		cases := map[string]*Rule{}
		var fallback *Rule
		for _, m := range s.Members {
			if value, ok := l.literalField(m, name); ok && cases[value] == nil {
				cases[value] = m
			} else if m.Kind == KindExcludeTags && m.Tag == name && fallback == nil {
				fallback = m
			} else {
				cases = nil
				break
			}
		}
		if cases == nil || (fallback != nil && !excludesAll(fallback, cases)) {
			continue
		}
		s.tag, s.cases, s.fallback = name, cases, fallback
		return
	}
}

// literalNames returns the properties of the first member that is an object,
// the candidates for the one that tells the members apart.
func (l *linker) literalNames(members []*Rule) []string {
	for _, m := range members {
		if o := l.object(m); o != nil {
			names := make([]string, len(o.Fields))
			for i, f := range o.Fields {
				names[i] = f.Name
			}
			return names
		}
	}
	return nil
}

// excludesAll reports whether the custom payload rule rejects every tag
// value that picks another member.
func excludesAll(fallback *Rule, cases map[string]*Rule) bool {
	for value := range cases {
		if !slices.Contains(fallback.Tags, value) {
			return false
		}
	}
	return true
}

// literalField returns the string literal the object m requires for name.
func (l *linker) literalField(m *Rule, name string) (string, bool) {
	o := l.object(m)
	if o == nil {
		return "", false
	}
	for _, f := range o.Fields {
		if f.Name != name || f.Schema.Kind != KindLiteral || f.Schema.Value.Kind() != '"' {
			continue
		}
		var value string
		if json.Unmarshal(f.Schema.Value, &value) != nil {
			return "", false
		}
		return value, true
	}
	return "", false
}

// Decode validates and normalizes raw with the named rule before decoding it into T.
func Decode[T any](r Registry, name string, raw []byte) (T, error) {
	var value T
	normalized, err := r.normalize(name, raw)
	if err != nil {
		return value, err
	}
	if err = json.Unmarshal(normalized, &value); err != nil {
		return value, fmt.Errorf("%s: decode normalized value: %w", name, err)
	}
	return value, nil
}

// Normalize applies the named rule and returns the normalized JSON.
func (r Registry) Normalize(name string, raw []byte) (jsontext.Value, error) {
	normalized, err := r.normalize(name, raw)
	return normalized.Clone(), err
}

// normalize is Normalize without the copy: the result may share raw's
// bytes, where the input needed no change.
func (r Registry) normalize(name string, raw []byte) (jsontext.Value, error) {
	if !jsontext.Value(raw).IsValid() {
		return nil, fmt.Errorf("%s: invalid JSON", name)
	}
	schema := r[name]
	if schema == nil {
		return nil, fmt.Errorf("unknown Zod schema %s", name)
	}
	result, err := r.apply(schema, parseTree(raw), nil, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if result.n == nil {
		return nil, fmt.Errorf("%s: top-level value became undefined", name)
	}
	if result.n.raw != nil {
		return bytes.TrimSpace(result.n.raw), nil
	}
	return encode(nil, result.n), nil
}

// Equal reports whether two JSON values are canonically equal.
func Equal(a, b jsontext.Value) bool {
	left, right := a.Clone(), b.Clone()
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if left.Canonicalize() != nil || right.Canonicalize() != nil {
		return false
	}
	return bytes.Equal(left, right)
}

// Unmarshaler returns a json.Unmarshalers entry that decodes values of T
// through Decode with the named rule. The normalized value is decoded without
// unmarshalers: the rule already covers nested values, so they are not
// normalized a second time.
func Unmarshaler[T any](r Registry, name string) *json.Unmarshalers {
	return json.UnmarshalFromFunc(func(dec *jsontext.Decoder, out *T) error {
		raw, err := dec.ReadValue()
		if err != nil {
			return err
		}
		v, err := Decode[T](r, name, raw)
		if err != nil {
			return err
		}
		*out = v
		return nil
	})
}
