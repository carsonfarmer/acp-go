package zod

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// SDK datetimes use RFC 3339 with seconds and optional arbitrary fractions.
var datetimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]([01]\d|2[0-3]):[0-5]\d)$`)

// outcome is what a rule made of a value: the value, or nil when it became
// absent, and the object properties the rule evaluated, which a custom
// payload leaves to the variant that declared them.
type outcome struct {
	n *node
	// object and more are the object rules evaluated, record any record:
	// together they say which properties were evaluated.
	object *Rule
	more   []*Rule
	record bool
}

// evaluated reports whether the rules that produced o evaluated the property
// name of their input.
func (o outcome) evaluated(name string) bool {
	if o.record {
		return true
	}
	for _, s := range append([]*Rule{o.object}, o.more...) {
		if s != nil && slices.ContainsFunc(s.Fields, func(f Field) bool { return f.Name == name }) {
			return true
		}
	}
	return false
}

// ruleError is a value a rule rejected. It is formatted only when read: an
// optional property's rule and a union's alternatives make and discard many.
type ruleError struct {
	path    *jsonPath
	message string
	value   []byte // the expected value, when the message names one
}

func (e *ruleError) Error() string {
	if e.value != nil {
		return fmt.Sprintf("%s: %s %s", e.path, e.message, e.value)
	}
	return fmt.Sprintf("%s: %s", e.path, e.message)
}

// jsonPath is where in the input a rule applies, built as evaluation descends
// and formatted only for an error. The root is nil.
type jsonPath struct {
	parent  *jsonPath
	name    string
	index   int
	element bool // index, not name, locates the value
}

func (p *jsonPath) String() string {
	if p == nil {
		return "$"
	}
	if p.element {
		return fmt.Sprintf("%s[%d]", p.parent, p.index)
	}
	return fmt.Sprintf("%s[%q]", p.parent, p.name)
}

// valueNode is the tree of a rule's default or fallback value, parsed by Link
// or, for a rule never linked, now.
func (s *Rule) valueNode() *node {
	if s.value != nil || s.Value == nil {
		return s.value
	}
	return parseTree(s.Value)
}

// apply evaluates s on n, the value at path, or nil for an absent one.
func (r Registry) apply(s *Rule, n *node, path *jsonPath, depth int) (outcome, error) {
	if depth > 512 {
		return outcome{}, fmt.Errorf("%s: schema nesting limit exceeded", path)
	}
	pass := func() (outcome, error) { return outcome{n: n}, nil }
	fail := func(message string) (outcome, error) { return outcome{}, &ruleError{path: path, message: message} }
	inner := func() (outcome, error) { return r.apply(s.Inner, n, path, depth+1) }
	switch s.Kind {
	case KindRef:
		target := s.target
		if target == nil {
			target = r[s.Ref]
		}
		if target == nil {
			return fail(fmt.Sprintf("unresolved schema %s", s.Ref))
		}
		return r.apply(target, n, path, depth+1)
	case KindOptional, KindNullish:
		if s.Kind == KindNullish && kindOf(n) == 'n' {
			return pass()
		}
		if n == nil {
			if s.Inner.needsValue {
				return pass()
			}
			result, err := inner()
			if err == nil {
				return result, nil
			}
			return pass()
		}
		return inner()
	case KindNullable:
		if kindOf(n) == 'n' {
			return pass()
		}
		return inner()
	case KindDefault:
		if n == nil {
			return outcome{n: s.valueNode()}, nil
		}
		return inner()
	case KindCatch, KindRequiredCatch:
		if s.Kind == KindRequiredCatch && n == nil {
			return fail("required value is missing")
		}
		if n == nil && s.Inner.needsValue {
			return outcome{n: s.valueNode()}, nil
		}
		value, err := inner()
		if err != nil {
			return outcome{n: s.valueNode()}, nil
		}
		return value, nil
	case KindUnknown, KindAny:
		return pass()
	case KindNever:
		return fail("value is not permitted")
	case KindUnion:
		if s.cases != nil {
			tag, found := stringMember(n, s.tag)
			if member := s.cases[tag]; found && member != nil {
				return r.apply(member, n, path, depth+1)
			}
			if s.fallback != nil {
				return r.apply(s.fallback, n, path, depth+1)
			}
			return fail(fmt.Sprintf("no union alternative matched: %q is not a known %s", tag, s.tag))
		}
		var problems []error
		for _, m := range s.Members {
			result, err := r.apply(m, n, path, depth+1)
			if err == nil {
				return result, nil
			}
			problems = append(problems, err)
		}
		return outcome{}, fmt.Errorf("%s: no union alternative matched: %w", path, errors.Join(problems...))
	case KindIntersection:
		if s.flat != nil {
			return r.apply(s.flat, n, path, depth+1)
		}
		result := outcome{}
		for i, m := range s.Members {
			next, err := r.apply(m, n, path, depth+1)
			if err != nil {
				return result, err
			}
			if i == 0 {
				result = next
				continue
			}
			merged, err := merge(result.n, next.n, path)
			if err != nil {
				return outcome{}, err
			}
			result.n = merged
			result.record = result.record || next.record
			for _, o := range append([]*Rule{next.object}, next.more...) {
				if o != nil {
					result.more = append(result.more, o)
				}
			}
		}
		return result, nil
	case KindOpenTags:
		// A Go-side extension: objects whose Tag is a string outside Tags skip
		// the SDK rule and decode into the union's Unknown variant.
		if tag, found := stringMember(n, s.Tag); found && !slices.Contains(s.Tags, tag) {
			return pass()
		}
		return inner()
	case KindExcludeTags, KindPreserve:
		result, err := inner()
		if err != nil {
			return result, err
		}
		tagged := n
		if s.Kind == KindExcludeTags {
			tagged = result.n
		}
		tag, found := stringMember(tagged, s.Tag)
		if !found {
			return result, nil
		}
		known := slices.Contains(s.Tags, tag)
		if s.Kind == KindExcludeTags {
			if known {
				return fail(fmt.Sprintf("%s %q is reserved by a known variant", s.Tag, tag))
			}
			return result, nil
		}
		if known {
			return result, nil
		}
		if kindOf(result.n) != '{' {
			return fail("custom payload must be an object")
		}
		// Restore the properties no variant evaluated.
		members := slices.Clone(result.n.members)
		for _, m := range n.members {
			if m.name == "__proto__" || result.evaluated(m.name) || result.n.get(m.name) != nil {
				continue
			}
			members = append(members, m)
		}
		if len(members) > len(result.n.members) {
			result.n = newObject(members)
		}
		return result, nil
	case KindMin, KindMax, KindGte, KindLte, KindRegex:
		result, err := inner()
		if err != nil {
			return result, err
		}
		if s.Kind == KindRegex {
			if kindOf(result.n) != '"' {
				return fail("expected string for pattern")
			}
			if !s.Regexp.MatchString(result.n.text()) {
				return fail("string does not match pattern")
			}
			return result, nil
		}
		var value float64
		switch kindOf(result.n) {
		case '0':
			var ok bool
			if value, ok = result.n.number(); !ok {
				return fail("invalid numeric value")
			}
		case '"':
			value = float64(utf8.RuneCountInString(result.n.text()))
		case '[':
			value = float64(len(result.n.elements))
		default:
			return fail("bound applied to unsupported value")
		}
		bound := s.bound
		if !s.linked {
			b, ok := (&node{kind: '0', raw: s.Value}).number()
			if !ok {
				return fail("invalid generated bound")
			}
			bound = b
		}
		if (s.Kind == KindMin || s.Kind == KindGte) && value < bound {
			return fail(fmt.Sprintf("value or length must be >= %g", bound))
		}
		if (s.Kind == KindMax || s.Kind == KindLte) && value > bound {
			return fail(fmt.Sprintf("value or length must be <= %g", bound))
		}
		return result, nil
	case KindInt:
		value := outcome{n: n}
		var err error
		if s.Inner != nil {
			value, err = inner()
			if err != nil {
				return value, err
			}
		}
		if kindOf(value.n) != '0' {
			return fail("expected a safe integer")
		}
		f, ok := value.n.number()
		if !ok || math.Trunc(f) != f || math.Abs(f) > 9007199254740991 {
			return fail("expected a safe integer")
		}
		// JSON numbers such as 1.0 and 1e0 are integers to JavaScript/Zod,
		// but must be normalized before decoding into a Go integer type.
		text := strconv.FormatFloat(f, 'f', 0, 64)
		if f == 0 {
			text = "0"
		}
		if text != string(value.n.raw) {
			value.n = &node{kind: '0', raw: []byte(text)}
		}
		return value, nil
	}
	if n == nil {
		return fail("required value is missing")
	}
	switch s.Kind {
	case KindNull:
		if n.kind != 'n' {
			return fail("expected null")
		}
	case KindString:
		if n.kind != '"' {
			return fail("expected string")
		}
	case KindBoolean:
		if n.kind != 't' && n.kind != 'f' {
			return fail("expected boolean")
		}
	case KindNumber:
		f, ok := 0.0, false
		if n.kind == '0' {
			f, ok = n.number()
		}
		if !ok || math.IsInf(f, 0) || math.IsNaN(f) {
			return fail("expected finite number")
		}
	case KindLiteral:
		if !s.matches(n) {
			return outcome{}, &ruleError{path: path, message: "expected literal", value: s.Value}
		}
	case KindURL, KindDateTime:
		if n.kind != '"' {
			return fail("expected string")
		}
		text := n.text()
		if s.Kind == KindURL {
			parsed, err := url.Parse(strings.TrimSpace(text))
			if err != nil || parsed.Scheme == "" {
				return fail("expected absolute URL")
			}
			switch strings.ToLower(parsed.Scheme) {
			case "http", "https", "ftp", "ws", "wss":
				if parsed.Hostname() == "" {
					return fail("URL requires a host")
				}
			}
		} else {
			if !datetimePattern.MatchString(text) {
				return fail("expected ISO datetime")
			}
			if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
				return fail("invalid datetime")
			}
			if !s.Offset && !strings.HasSuffix(text, "Z") {
				return fail("datetime offset is not allowed")
			}
		}
	case KindArray, KindSkipArray:
		if n.kind != '[' {
			return fail("expected array")
		}
		var output []*node // nil while every element is unchanged
		for i, element := range n.elements {
			result, err := r.apply(s.Inner, element, &jsonPath{parent: path, index: i, element: true}, depth+1)
			if err == nil && result.n == nil {
				result.n = &node{kind: 'n', raw: []byte("null")}
			}
			if output == nil && (err != nil || result.n != element) {
				output = append(make([]*node, 0, len(n.elements)), n.elements[:i]...)
			}
			if err != nil {
				if s.Kind == KindSkipArray {
					continue
				}
				return outcome{}, err
			}
			if output != nil {
				output = append(output, result.n)
			}
		}
		if output == nil {
			return pass()
		}
		return outcome{n: &node{kind: '[', elements: output}}, nil
	case KindObject:
		if n.kind != '{' {
			return fail("expected object")
		}
		output := make([]member, 0, len(s.Fields))
		unchanged := len(n.members) <= len(s.Fields)
		for _, field := range s.Fields {
			input := n.get(field.Name)
			value, err := r.apply(field.Schema, input, &jsonPath{parent: path, name: field.Name}, depth+1)
			if err != nil {
				return outcome{}, err
			}
			if value.n != nil {
				output = append(output, member{field.Name, value.n})
			}
			unchanged = unchanged && value.n == input
		}
		// Unchanged means every property of the input is a field whose
		// value came back as it was, so the input is the output.
		if unchanged && len(output) == len(n.members) {
			return outcome{n: n, object: s}, nil
		}
		return outcome{n: newObject(output), object: s}, nil
	case KindRecord:
		if n.kind != '{' {
			return fail("expected object")
		}
		// Keys in sorted order, so the first invalid one reported does not
		// depend on the input's order.
		order := make([]int, len(n.members))
		for i := range order {
			order[i] = i
		}
		slices.SortFunc(order, func(a, b int) int { return strings.Compare(n.members[a].name, n.members[b].name) })
		output := make([]member, 0, len(n.members))
		unchanged := true
		for _, i := range order {
			m := n.members[i]
			at := &jsonPath{parent: path, name: m.name}
			if _, err := r.apply(s.Key, keyNode(m.name), at, depth+1); err != nil {
				return outcome{}, err
			}
			value, err := r.apply(s.Inner, m.value, at, depth+1)
			if err != nil {
				return outcome{}, err
			}
			if value.n != nil {
				output = append(output, member{m.name, value.n})
			}
			unchanged = unchanged && value.n == m.value
		}
		if unchanged {
			return outcome{n: n, record: true}, nil
		}
		return outcome{n: newObject(output), record: true}, nil
	default:
		return fail(fmt.Sprintf("unsupported generated Zod rule %d", s.Kind))
	}
	return pass()
}

// matches reports whether n equals the literal s.
func (s *Rule) matches(n *node) bool {
	// A string without escapes is already in canonical form.
	if s.lit != nil && n.kind == '"' && s.lit[0] == '"' && !slices.Contains(n.raw, '\\') {
		return string(n.raw) == string(s.lit)
	}
	return Equal(encode(nil, n), s.Value)
}

// keyNode is a record key as a string value for the key rule.
func keyNode(name string) *node {
	raw, _ := jsontext.AppendQuote(nil, name)
	return &node{kind: '"', raw: raw}
}

// stringMember returns the string property name of an object, or false when
// n is not an object or has no such string property.
func stringMember(n *node, name string) (string, bool) {
	if kindOf(n) != '{' {
		return "", false
	}
	v := n.get(name)
	if kindOf(v) != '"' {
		return "", false
	}
	return v.text(), true
}

// merge combines the results of an intersection's members.
func merge(a, b *node, path *jsonPath) (*node, error) {
	if same(a, b) {
		return a, nil
	}
	switch {
	case kindOf(a) == '{' && kindOf(b) == '{':
		members := slices.Clone(a.members)
		for _, m := range b.members {
			i := slices.IndexFunc(members, func(x member) bool { return x.name == m.name })
			if i < 0 {
				members = append(members, m)
				continue
			}
			merged, err := merge(members[i].value, m.value, &jsonPath{parent: path, name: m.name})
			if err != nil {
				return nil, err
			}
			members[i].value = merged
		}
		return newObject(members), nil
	case kindOf(a) == '[' && kindOf(b) == '[' && len(a.elements) == len(b.elements):
		elements := make([]*node, len(a.elements))
		for i := range elements {
			merged, err := merge(a.elements[i], b.elements[i], &jsonPath{parent: path, index: i, element: true})
			if err != nil {
				return nil, err
			}
			elements[i] = merged
		}
		return &node{kind: '[', elements: elements}, nil
	}
	return nil, fmt.Errorf("%s: incompatible intersection results", path)
}
