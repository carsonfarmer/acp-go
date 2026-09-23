package tsdef

import "testing"

func TestNumericHints(t *testing.T) {
	s, err := Parse("types.ts", []byte(`export type Version = number; export type Request = {line?: number | null; cost: number; count: number;};`))
	if err != nil {
		t.Fatal(err)
	}
	err = ApplyNumericHints(s, "zod.ts", []byte(`
 export const zVersion = z.int().gte(0).lte(65535);
 export const zRequest = z.object({line: defaultOnError(z.number().int().gte(0).max(4294967295).nullish(),()=>undefined),cost:z.number(),count:z.int()});
 `))
	if err != nil {
		t.Fatal(err)
	}
	if s.Types[0].Type.Number != "uint16" {
		t.Fatal("missing protocol version range")
	}
	fields := s.Types[1].Type.Fields
	if fields[0].Type.Members[0].Number != "uint32" || fields[1].Type.Number != "" || fields[2].Type.Number != "int64" {
		t.Fatal("incorrect numeric representations")
	}
}
