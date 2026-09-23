package zod

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"
)

var treeSamples = []string{
	`null`, `true`, `false`, `0`, `-1.5e-3`, `1E+2`, `""`, `"a\"b\\cé😀"`,
	`{}`, `[]`, ` { "a" : [ 1 , { "b" : null } ] , "c" : "d" } `,
	`{"ab":1,"c\n":[[],{}]}`,
	`[1,"two",{"three":3},[4],true,null]`,
}

// A parsed tree encodes back to the same JSON, and finds every member of
// an object, indexed or not.
func TestTreeRoundTrip(t *testing.T) {
	var many strings.Builder
	many.WriteString("{")
	for i := range indexFrom * 2 {
		if i > 0 {
			many.WriteString(",")
		}
		fmt.Fprintf(&many, `"k%d":%d`, i, i)
	}
	many.WriteString("}")
	for _, sample := range append(treeSamples, many.String()) {
		n := parseTree([]byte(sample))
		if got := encode(nil, n); !Equal(got, jsontext.Value(sample)) {
			t.Errorf("encode(parse(%s)) = %s", sample, got)
		}
		if n.kind == '{' {
			for _, m := range n.members {
				if n.get(m.name) != m.value {
					t.Errorf("%s: get(%q) did not find its member", sample, m.name)
				}
			}
			if n.get("missing") != nil {
				t.Errorf("%s: get found a missing member", sample)
			}
		}
	}
	if got := parseTree([]byte(`{"ab":1}`)).members[0].name; got != "ab" {
		t.Errorf("escaped name decoded as %q", got)
	}
}

// A built node, which has no text of its own, encodes from its members.
func TestTreeEncodeBuilt(t *testing.T) {
	inner := parseTree([]byte(`[1, 2]`))
	built := newObject([]member{{"a\"", inner}, {"b", &node{kind: '[', elements: []*node{inner}}}})
	if got := encode(nil, built); !Equal(got, jsontext.Value(`{"a\"":[1,2],"b":[[1,2]]}`)) {
		t.Errorf("encode = %s", got)
	}
}

func FuzzTree(f *testing.F) {
	for _, sample := range treeSamples {
		f.Add(sample)
	}
	f.Fuzz(func(t *testing.T, data string) {
		if !jsontext.Value(data).IsValid() {
			return
		}
		if got := encode(nil, parseTree([]byte(data))); !Equal(got, jsontext.Value(data)) {
			t.Fatalf("encode(parse(%q)) = %q", data, got)
		}
	})
}
