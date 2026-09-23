// Package zod evaluates the statically extracted Zod rules used by the
// generated ACP schema packages. It is a runtime dependency of those packages
// rather than a general Zod implementation, internal so that it can change
// with them.
package zod

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
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

// stringMember scans the object raw for the string property name. It reads
// the whole object, so a malformed object or a duplicate name is an error as
// it is when the object is decoded. found is false when raw is not an object
// or name is absent or not a string.
func stringMember(raw jsontext.Value, name string) (value string, found bool, err error) {
	if raw.Kind() != '{' {
		return "", false, nil
	}
	d := decoders.Get().(*jsontext.Decoder)
	d.Reset(bytes.NewReader(raw))
	defer func() {
		d.Reset(bytes.NewReader(nil))
		decoders.Put(d)
	}()
	if _, err := d.ReadToken(); err != nil {
		return "", false, err
	}
	for d.PeekKind() != '}' {
		key, err := d.ReadToken()
		if err != nil {
			return "", false, err
		}
		if key.String() == name && d.PeekKind() == '"' && !found {
			tok, err := d.ReadToken()
			if err != nil {
				return "", false, err
			}
			value, found = tok.String(), true
			continue
		}
		if err := d.SkipValue(); err != nil {
			return "", false, err
		}
	}
	if _, err := d.ReadToken(); err != nil {
		return "", false, err
	}
	return value, found, nil
}

// ruleError is a value a rule rejected. It is formatted only when read: an
// optional property's rule and a union's alternatives make and discard many.
type ruleError struct {
	path    *jsonPath
	message string
	value   jsontext.Value // the expected value, when the message names one
}

func (e *ruleError) Error() string {
	if e.value != nil {
		return fmt.Sprintf("%s: %s %s", e.path, e.message, e.value)
	}
	return fmt.Sprintf("%s: %s", e.path, e.message)
}

// decoders reuses the decoders stringMember scans objects with.
var decoders = sync.Pool{New: func() any { return new(jsontext.Decoder) }}

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

type outcome struct {
	raw       jsontext.Value
	evaluated map[string]bool
}

// SDK datetimes use RFC 3339 with seconds and optional arbitrary fractions.
var datetimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]([01]\d|2[0-3]):[0-5]\d)$`)

// decodeZod validates and normalizes input before decoding its Go wire type.
// Decode validates and normalizes raw with the named rule before decoding it into T.
func Decode[T any](r Registry, name string, raw []byte) (T, error) {
	var value T
	normalized, err := r.Normalize(name, raw)
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
	if !jsontext.Value(raw).IsValid() {
		return nil, fmt.Errorf("%s: invalid JSON", name)
	}
	schema := r[name]
	if schema == nil {
		return nil, fmt.Errorf("unknown Zod schema %s", name)
	}
	result, err := r.apply(schema, jsontext.Value(raw), nil, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if result.raw == nil {
		return nil, fmt.Errorf("%s: top-level value became undefined", name)
	}
	return result.raw, nil
}
func (r Registry) apply(s *Rule, raw jsontext.Value, path *jsonPath, depth int) (outcome, error) {
	if depth > 512 {
		return outcome{}, fmt.Errorf("%s: schema nesting limit exceeded", path)
	}
	pass := func() (outcome, error) { return outcome{raw: raw}, nil }
	fail := func(message string) (outcome, error) { return outcome{}, &ruleError{path: path, message: message} }
	inner := func() (outcome, error) { return r.apply(s.Inner, raw, path, depth+1) }
	switch s.Kind {
	case KindRef:
		target := s.target
		if target == nil {
			target = r[s.Ref]
		}
		if target == nil {
			return fail(fmt.Sprintf("unresolved schema %s", s.Ref))
		}
		return r.apply(target, raw, path, depth+1)
	case KindOptional, KindNullish:
		if s.Kind == KindNullish && raw.Kind() == 'n' {
			return pass()
		}
		if raw == nil {
			result, err := inner()
			if err == nil {
				return result, nil
			}
			return pass()
		}
		return inner()
	case KindNullable:
		if raw.Kind() == 'n' {
			return pass()
		}
		return inner()
	case KindDefault:
		if raw == nil {
			return outcome{raw: s.Value.Clone()}, nil
		}
		return inner()
	case KindCatch, KindRequiredCatch:
		if s.Kind == KindRequiredCatch && raw == nil {
			return fail("required value is missing")
		}
		value, err := inner()
		if err != nil {
			return outcome{raw: s.Value.Clone()}, nil
		}
		return value, nil
	case KindUnknown, KindAny:
		return pass()
	case KindNever:
		return fail("value is not permitted")
	case KindUnion:
		if s.cases != nil {
			tag, found, err := stringMember(raw, s.tag)
			if err != nil {
				return outcome{}, fmt.Errorf("%s: %w", path, err)
			}
			if member := s.cases[tag]; found && member != nil {
				return r.apply(member, raw, path, depth+1)
			}
			if s.fallback != nil {
				return r.apply(s.fallback, raw, path, depth+1)
			}
			return fail(fmt.Sprintf("no union alternative matched: %q is not a known %s", tag, s.tag))
		}
		var problems []error
		for _, m := range s.Members {
			result, err := r.apply(m, raw, path, depth+1)
			if err == nil {
				return result, nil
			}
			problems = append(problems, err)
		}
		return outcome{}, fmt.Errorf("%s: no union alternative matched: %w", path, errors.Join(problems...))
	case KindIntersection:
		if s.flat != nil {
			return r.apply(s.flat, raw, path, depth+1)
		}
		result := outcome{}
		for i, m := range s.Members {
			next, err := r.apply(m, raw, path, depth+1)
			if err != nil {
				return result, err
			}
			if i == 0 {
				result = next
				continue
			}
			merged, err := merge(result.raw, next.raw, path)
			if err != nil {
				return outcome{}, err
			}
			result.raw = merged
			if result.evaluated == nil {
				result.evaluated = map[string]bool{}
			}
			for k := range next.evaluated {
				result.evaluated[k] = true
			}
		}
		return result, nil
	case KindOpenTags:
		// A Go-side extension: objects whose Tag is a string outside Tags skip
		// the SDK rule and decode into the union's Unknown variant.
		tag, found, err := stringMember(raw, s.Tag)
		if err != nil {
			return outcome{}, err
		}
		if found && !slices.Contains(s.Tags, tag) {
			return pass()
		}
		return inner()
	case KindExcludeTags, KindPreserve:
		result, err := inner()
		if err != nil {
			return result, err
		}
		var fields map[string]jsontext.Value
		tagRaw := raw
		if s.Kind == KindExcludeTags {
			tagRaw = result.raw
		}
		if tagRaw.Kind() != '{' {
			return result, nil
		}
		if err = json.Unmarshal(tagRaw, &fields); err != nil {
			return outcome{}, err
		}
		tagValue := fields[s.Tag]
		if tagValue.Kind() != '"' {
			return result, nil
		}
		var tag string
		if err = json.Unmarshal(tagValue, &tag); err != nil {
			return outcome{}, err
		}
		known := slices.Contains(s.Tags, tag)
		if s.Kind == KindExcludeTags {
			if known {
				return fail(fmt.Sprintf("%s %q is reserved by a known variant", s.Tag, tag))
			}
			return result, nil
		}
		if !known {
			var output map[string]jsontext.Value
			if err = json.Unmarshal(result.raw, &output); err != nil {
				return outcome{}, err
			}
			if output == nil {
				return fail("custom payload must be an object")
			}
			for key, value := range fields {
				if key == "__proto__" || result.evaluated[key] {
					continue
				}
				if _, exists := output[key]; !exists {
					output[key] = value
				}
			}
			result.raw, err = json.Marshal(output, json.Deterministic(true))
			if err != nil {
				return outcome{}, err
			}
		}
		return result, nil
	case KindMin, KindMax, KindGte, KindLte, KindRegex:
		result, err := inner()
		if err != nil {
			return result, err
		}
		if s.Kind == KindRegex {
			var value string
			if result.raw.Kind() != '"' || json.Unmarshal(result.raw, &value) != nil {
				return fail("expected string for pattern")
			}
			if !s.Regexp.MatchString(value) {
				return fail("string does not match pattern")
			}
			return result, nil
		}
		var value float64
		switch result.raw.Kind() {
		case '0':
			if err = json.Unmarshal(result.raw, &value); err != nil {
				return fail("invalid numeric value")
			}
		case '"':
			var text string
			if err = json.Unmarshal(result.raw, &text); err != nil {
				return fail("invalid string")
			}
			value = float64(utf8.RuneCountInString(text))
		case '[':
			var items []jsontext.Value
			if err = json.Unmarshal(result.raw, &items); err != nil {
				return fail("invalid array")
			}
			value = float64(len(items))
		default:
			return fail("bound applied to unsupported value")
		}
		bound := s.bound
		if !s.linked && json.Unmarshal(s.Value, &bound) != nil {
			return fail("invalid generated bound")
		}
		if (s.Kind == KindMin || s.Kind == KindGte) && value < bound {
			return fail(fmt.Sprintf("value or length must be >= %g", bound))
		}
		if (s.Kind == KindMax || s.Kind == KindLte) && value > bound {
			return fail(fmt.Sprintf("value or length must be <= %g", bound))
		}
		return result, nil
	case KindInt:
		value := outcome{raw: raw}
		var err error
		if s.Inner != nil {
			value, err = inner()
			if err != nil {
				return value, err
			}
		}
		var n float64
		if value.raw.Kind() != '0' || json.Unmarshal(value.raw, &n) != nil || math.Trunc(n) != n || math.Abs(n) > 9007199254740991 {
			return fail("expected a safe integer")
		}
		// JSON numbers such as 1.0 and 1e0 are integers to JavaScript/Zod,
		// but must be normalized before decoding into a Go integer type.
		value.raw = jsontext.Value(fmt.Sprintf("%.0f", n))
		if n == 0 {
			value.raw = jsontext.Value("0")
		}
		return value, nil
	}
	if raw == nil {
		return fail("required value is missing")
	}
	switch s.Kind {
	case KindNull:
		if raw.Kind() != 'n' {
			return fail("expected null")
		}
	case KindString:
		if raw.Kind() != '"' {
			return fail("expected string")
		}
	case KindBoolean:
		if raw.Kind() != 't' && raw.Kind() != 'f' {
			return fail("expected boolean")
		}
	case KindNumber:
		var n float64
		if raw.Kind() != '0' || json.Unmarshal(raw, &n) != nil || math.IsInf(n, 0) || math.IsNaN(n) {
			return fail("expected finite number")
		}
	case KindLiteral:
		// A string without escapes is already in canonical form.
		if s.lit != nil && raw.Kind() == '"' && s.lit.Kind() == '"' && bytes.IndexByte(raw, '\\') < 0 {
			if !bytes.Equal(raw, s.lit) {
				return outcome{}, &ruleError{path: path, message: "expected literal", value: s.Value}
			}
		} else if !Equal(raw, s.Value) {
			return outcome{}, &ruleError{path: path, message: "expected literal", value: s.Value}
		}
	case KindURL, KindDateTime:
		var text string
		if raw.Kind() != '"' || json.Unmarshal(raw, &text) != nil {
			return fail("expected string")
		}
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
			// SDK datetimes use RFC 3339 with seconds and optional arbitrary fractions.
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
		if raw.Kind() != '[' {
			return fail("expected array")
		}
		var input []jsontext.Value
		if err := json.Unmarshal(raw, &input); err != nil {
			return outcome{}, err
		}
		output := make([]jsontext.Value, 0, len(input))
		for i, item := range input {
			result, err := r.apply(s.Inner, item, &jsonPath{parent: path, index: i, element: true}, depth+1)
			if err != nil {
				if s.Kind == KindSkipArray {
					continue
				}
				return outcome{}, err
			}
			if result.raw == nil {
				result.raw = jsontext.Value("null")
			}
			output = append(output, result.raw)
		}
		encoded, err := json.Marshal(output)
		return outcome{raw: encoded}, err
	case KindObject, KindRecord:
		if raw.Kind() != '{' {
			return fail("expected object")
		}
		var input map[string]jsontext.Value
		if err := json.Unmarshal(raw, &input); err != nil {
			return outcome{}, err
		}
		output := map[string]jsontext.Value{}
		evaluated := map[string]bool{}
		if s.Kind == KindObject {
			for _, field := range s.Fields {
				value, err := r.apply(field.Schema, input[field.Name], &jsonPath{parent: path, name: field.Name}, depth+1)
				if err != nil {
					return outcome{}, err
				}
				if value.raw != nil {
					output[field.Name] = value.raw
				}
				_, present := input[field.Name]
				if present || value.raw != nil {
					evaluated[field.Name] = true
				}
			}
		} else {
			keys := make([]string, 0, len(input))
			for k := range input {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			for _, key := range keys {
				encoded, _ := json.Marshal(key)
				at := &jsonPath{parent: path, name: key}
				if _, err := r.apply(s.Key, encoded, at, depth+1); err != nil {
					return outcome{}, err
				}
				value, err := r.apply(s.Inner, input[key], at, depth+1)
				if err != nil {
					return outcome{}, err
				}
				if value.raw != nil {
					output[key] = value.raw
				}
				evaluated[key] = true
			}
		}
		encoded, err := json.Marshal(output, json.Deterministic(true))
		return outcome{raw: encoded, evaluated: evaluated}, err
	default:
		return fail(fmt.Sprintf("unsupported generated Zod rule %d", s.Kind))
	}
	return pass()
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
func merge(a, b jsontext.Value, path *jsonPath) (jsontext.Value, error) {
	if Equal(a, b) {
		return a, nil
	}
	if a.Kind() == '{' && b.Kind() == '{' {
		var left, right map[string]jsontext.Value
		if err := json.Unmarshal(a, &left); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &right); err != nil {
			return nil, err
		}
		for key, value := range right {
			if old, ok := left[key]; ok {
				merged, err := merge(old, value, &jsonPath{parent: path, name: key})
				if err != nil {
					return nil, err
				}
				left[key] = merged
			} else {
				left[key] = value
			}
		}
		return json.Marshal(left, json.Deterministic(true))
	}
	if a.Kind() == '[' && b.Kind() == '[' {
		var left, right []jsontext.Value
		if err := json.Unmarshal(a, &left); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &right); err != nil {
			return nil, err
		}
		if len(left) == len(right) {
			for i := range left {
				merged, err := merge(left[i], right[i], &jsonPath{parent: path, index: i, element: true})
				if err != nil {
					return nil, err
				}
				left[i] = merged
			}
			return json.Marshal(left)
		}
	}
	return nil, fmt.Errorf("%s: incompatible intersection results", path)
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
