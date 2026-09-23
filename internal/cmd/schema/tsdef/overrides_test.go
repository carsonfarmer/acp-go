package tsdef

import "testing"

func TestOverrides(t *testing.T) {
	schema, err := Parse("fixture.ts", []byte(`export type Usage = { total: number; cached?: number | null; label: string; };`))
	if err != nil {
		t.Fatal(err)
	}
	o, err := ParseOverrides([]byte("numbers:\n  Usage.total: uint64\n  Usage.cached: int64\n  Missing.field: uint64\n"))
	if err != nil {
		t.Fatal(err)
	}
	applied, err := o.Apply(schema)
	if err != nil {
		t.Fatal(err)
	}
	if !applied["Usage.total"] || !applied["Usage.cached"] || applied["Missing.field"] {
		t.Fatalf("applied %v", applied)
	}
	fields := schema.Types[0].Type.Fields
	if fields[0].Type.Number != "uint64" || numberMember(fields[1].Type).Number != "int64" {
		t.Fatalf("numbers not set: %+v %+v", fields[0].Type, fields[1].Type)
	}

	if _, err := (&Overrides{Numbers: map[string]string{"Usage.label": "uint64"}}).Apply(schema); err == nil {
		t.Fatal("overrode a string member")
	}
	for _, bad := range []string{"numbers:\n  Usage.total: float32\n", "numbers:\n  Usage: uint64\n", "strings: {}\n"} {
		if _, err := ParseOverrides([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
