// Package zod evaluates the statically extracted Zod rules used by the
// generated ACP schema packages. It is a runtime dependency of those packages
// rather than a general Zod implementation; its API may change with them.
package zod

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strings"
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
}

// Field is a named object property.
type Field struct {
	Name   string
	Schema *Rule
}

// Registry maps schema names to their top-level rules.
type Registry map[string]*Rule

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
	result, err := r.apply(schema, jsontext.Value(raw), "$", 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if result.raw == nil {
		return nil, fmt.Errorf("%s: top-level value became undefined", name)
	}
	return result.raw, nil
}
func (r Registry) apply(s *Rule, raw jsontext.Value, path string, depth int) (outcome, error) {
	if depth > 512 {
		return outcome{}, fmt.Errorf("%s: schema nesting limit exceeded", path)
	}
	pass := func() (outcome, error) { return outcome{raw: raw}, nil }
	fail := func(message string) (outcome, error) { return outcome{}, fmt.Errorf("%s: %s", path, message) }
	inner := func() (outcome, error) { return r.apply(s.Inner, raw, path, depth+1) }
	switch s.Kind {
	case KindRef:
		target := r[s.Ref]
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
		var bound float64
		if err = json.Unmarshal(s.Value, &bound); err != nil {
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
		if !Equal(raw, s.Value) {
			return fail(fmt.Sprintf("expected literal %s", s.Value))
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
			result, err := r.apply(s.Inner, item, fmt.Sprintf("%s[%d]", path, i), depth+1)
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
				value, err := r.apply(field.Schema, input[field.Name], fmt.Sprintf("%s[%q]", path, field.Name), depth+1)
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
				if _, err := r.apply(s.Key, encoded, fmt.Sprintf("%s[%q]", path, key), depth+1); err != nil {
					return outcome{}, err
				}
				value, err := r.apply(s.Inner, input[key], fmt.Sprintf("%s[%q]", path, key), depth+1)
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
func merge(a, b jsontext.Value, path string) (jsontext.Value, error) {
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
				merged, err := merge(old, value, fmt.Sprintf("%s[%q]", path, key))
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
				merged, err := merge(left[i], right[i], fmt.Sprintf("%s[%d]", path, i))
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
