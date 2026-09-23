// Package zodsnap pins what a [zod.Registry] makes of a corpus of inputs, so a
// change to the rule evaluator or to how rules are generated can be checked
// against the results before it. Only the schema packages' tests import it.
//
// The corpus is derived from the rules: a sample value for every schema, one
// per alternative of a top-level union, and mutations of each sample that drop
// a property, null it, or give it a value of the wrong kind. The inputs are
// saved with their results, so a later check replays the same inputs however
// the rules have changed since. The file is gzipped JSON with one case per
// line; zcat it to read or diff it.
package zodsnap

import (
	"bytes"
	"compress/gzip"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"flag"
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/ironpark/acp-go/schema/internal/zod"
)

var update = flag.Bool("update-zod-snapshot", false, "rewrite the Zod snapshot from the current rules and evaluator")

// maxDepth bounds how many references a sample follows, so recursive schemas
// end.
const maxDepth = 6

// Case is one input to a schema and what Normalize made of it: the normalized
// value, Same when that is the input itself, or neither when the schema
// rejected the input.
type Case struct {
	Schema string         `json:"schema"`
	Input  jsontext.Value `json:"in"`
	Output jsontext.Value `json:"out,omitzero"`
	Same   bool           `json:"same,omitzero"`
}

// Check compares r's results on the corpus saved at path with the saved ones.
// Run the test with -update-zod-snapshot to rewrite the file from r.
func Check(t *testing.T, r zod.Registry, path string) {
	t.Helper()
	if *update {
		if err := write(path, run(r, Corpus(r))); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := read(path)
	if err != nil {
		t.Fatalf("%v; run the test with -update-zod-snapshot to create it", err)
	}
	mismatches := 0
	for _, c := range want {
		if c.Same {
			c.Output = c.Input
		}
		got, err := r.Normalize(c.Schema, c.Input)
		if err != nil {
			got = nil
		}
		if (got == nil) != (c.Output == nil) || (got != nil && !zod.Equal(got, c.Output)) {
			mismatches++
			if mismatches <= 20 {
				t.Errorf("%s(%s) = %s (err %v), want %s", c.Schema, c.Input, got, err, orRejected(c.Output))
			}
		}
	}
	if mismatches > 20 {
		t.Errorf("... %d mismatches of %d cases", mismatches, len(want))
	}
}

func orRejected(v jsontext.Value) string {
	if v == nil {
		return "rejected"
	}
	return string(v)
}

func run(r zod.Registry, inputs []Case) []Case {
	for i, c := range inputs {
		out, err := r.Normalize(c.Schema, c.Input)
		switch {
		case err != nil:
		case zod.Equal(out, c.Input):
			inputs[i].Same = true
		default:
			inputs[i].Output = out
		}
	}
	return inputs
}

func read(path string) ([]Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	var cases []Case
	err = json.UnmarshalRead(zr, &cases)
	return cases, err
}

// write saves the cases one per line, so a changed result shows as a
// one-line diff of the unzipped files.
func write(path string, cases []Case) error {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write([]byte("[\n"))
	for i, c := range cases {
		line, err := json.Marshal(c, json.Deterministic(true))
		if err != nil {
			return err
		}
		if i < len(cases)-1 {
			line = append(line, ',')
		}
		zw.Write(append(line, '\n'))
	}
	zw.Write([]byte("]\n"))
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// Corpus returns the inputs for every schema in r, in a stable order.
func Corpus(r zod.Registry) []Case {
	s := &sampler{r: r}
	var cases []Case
	for _, name := range slices.Sorted(maps.Keys(r)) {
		seen := map[string]bool{}
		add := func(input jsontext.Value) {
			if !seen[string(input)] {
				seen[string(input)] = true
				cases = append(cases, Case{Schema: name, Input: input})
			}
		}
		for _, v := range []string{`null`, `"x"`, `1.5`, `[]`, `{}`} {
			add(jsontext.Value(v))
		}
		for _, sample := range s.alternatives(r[name]) {
			add(sample)
			for _, mutated := range mutations(sample) {
				add(mutated)
			}
		}
	}
	return cases
}

// wrongValues replace a property's value in the mutations: values of other
// kinds, and 2.0, an integer written as a float.
var wrongValues = []string{`null`, `"x"`, `0.5`, `2.0`, `{}`}

// mutations returns sample with each property in turn dropped or replaced by
// each wrong value, and with an unknown property added.
func mutations(sample jsontext.Value) []jsontext.Value {
	var fields map[string]jsontext.Value
	if sample.Kind() != '{' || json.Unmarshal(sample, &fields) != nil {
		return nil
	}
	encode := func(m map[string]jsontext.Value) jsontext.Value {
		data, _ := json.Marshal(m, json.Deterministic(true))
		return data
	}
	var out []jsontext.Value
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		without := maps.Clone(fields)
		delete(without, key)
		out = append(out, encode(without))
		for _, v := range wrongValues {
			changed := maps.Clone(fields)
			changed[key] = jsontext.Value(v)
			out = append(out, encode(changed))
		}
	}
	extra := maps.Clone(fields)
	extra["zzUnknown"] = jsontext.Value(`1`)
	return append(out, encode(extra))
}

// sampler builds a value each rule accepts where it can: every property
// present, one element per array, and union alternatives taken in turn.
type sampler struct {
	r    zod.Registry
	turn int
}

// alternatives returns one sample per alternative of a top-level union, or
// one sample for any other rule.
func (s *sampler) alternatives(rule *zod.Rule) []jsontext.Value {
	for rule.Kind == zod.KindOpenTags || rule.Kind == zod.KindExcludeTags || rule.Kind == zod.KindPreserve {
		rule = rule.Inner
	}
	if rule.Kind != zod.KindUnion {
		return []jsontext.Value{s.sample(rule, 0)}
	}
	var out []jsontext.Value
	for _, m := range rule.Members {
		out = append(out, s.sample(m, 0))
	}
	return out
}

func (s *sampler) sample(rule *zod.Rule, depth int) jsontext.Value {
	switch rule.Kind {
	case zod.KindRef:
		if depth >= maxDepth || s.r[rule.Ref] == nil {
			return jsontext.Value(`null`)
		}
		return s.sample(s.r[rule.Ref], depth+1)
	case zod.KindOptional, zod.KindNullish, zod.KindNullable, zod.KindDefault, zod.KindCatch,
		zod.KindRequiredCatch, zod.KindExcludeTags, zod.KindPreserve, zod.KindOpenTags,
		zod.KindMin, zod.KindMax, zod.KindGte, zod.KindLte:
		return s.sample(rule.Inner, depth)
	case zod.KindRegex:
		return jsontext.Value(`"a"`)
	case zod.KindInt:
		return jsontext.Value(`1`)
	case zod.KindUnknown, zod.KindAny:
		return jsontext.Value(`{"any":1}`)
	case zod.KindNever, zod.KindNull:
		return jsontext.Value(`null`)
	case zod.KindString:
		return jsontext.Value(`"s"`)
	case zod.KindBoolean:
		return jsontext.Value(`true`)
	case zod.KindNumber:
		return jsontext.Value(`1.5`)
	case zod.KindLiteral:
		return rule.Value
	case zod.KindURL:
		return jsontext.Value(`"https://example.com/a"`)
	case zod.KindDateTime:
		return jsontext.Value(`"2024-01-02T03:04:05Z"`)
	case zod.KindUnion:
		s.turn++
		return s.sample(rule.Members[s.turn%len(rule.Members)], depth)
	case zod.KindIntersection:
		merged := map[string]jsontext.Value{}
		for _, m := range rule.Members {
			var fields map[string]jsontext.Value
			part := s.sample(m, depth)
			if json.Unmarshal(part, &fields) != nil {
				return part
			}
			maps.Copy(merged, fields)
		}
		data, _ := json.Marshal(merged, json.Deterministic(true))
		return data
	case zod.KindArray, zod.KindSkipArray:
		data, _ := json.Marshal([]jsontext.Value{s.sample(rule.Inner, depth)})
		return data
	case zod.KindObject:
		fields := map[string]jsontext.Value{}
		for _, f := range rule.Fields {
			fields[f.Name] = s.sample(f.Schema, depth)
		}
		data, _ := json.Marshal(fields, json.Deterministic(true))
		return data
	case zod.KindRecord:
		data, _ := json.Marshal(map[string]jsontext.Value{"k": s.sample(rule.Inner, depth)})
		return data
	}
	return jsontext.Value(`null`)
}
