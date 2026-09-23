package tsgen

import (
	"maps"
	"os"
	"os/exec"
	"slices"
	"testing"

	"github.com/ironpark/go-acp/internal/cmd/schema/tsdef"
)

func TestGeneratedZod(t *testing.T) {
	s, err := tsdef.Parse("types.ts", []byte(`
 export type Count = number;
 export type Currency = string;
 export type URL = string;
 export type Timestamp = string;
 export type Options = { count: Count; label?: string; required: Array<number>; url?: URL; };
 export type Text = { text: string; note?: string | null; };
 export type Message = ((Text & {kind: "text"}) | {kind: string; [key:string]:unknown;}) & {_meta?: {[key:string]:unknown}|null;};
 `))
	if err != nil {
		t.Fatal(err)
	}
	s.Validators, err = tsdef.ParseZod("zod.ts", []byte(`
 export const zCount = z.int().gte(0).max(10);
 export const zCurrency = z.string().regex(/^[A-Z]{3}$/);
 export const zURL = z.url();
 export const zTimestamp = z.iso.datetime({offset:true});
 export const zOptions = z.object({
  count: zCount.default(3),
  label: z.string().optional().default("hello"),
  required: requiredDefaultOnError(vecSkipError(z.number().int()),()=>[]),
  url: defaultOnError(zURL.nullish(),()=>undefined),
 });
 export const zText = z.object({text:z.string(),note:defaultOnError(z.string().nullish(),()=>undefined)});
 export const zMessage = preserveCustomPayload(z.intersection(z.union([
  zText.and(z.object({kind:z.literal("text")})),
  excludeKnownTags(z.object({kind:z.string()}),"kind",["text"]),
 ]),z.object({_meta:defaultOnError(z.record(z.string(),z.unknown()).nullish(),()=>undefined)})),"kind",["text"]);
 `))
	if err != nil {
		t.Fatal(err)
	}
	data, err := Generate(s, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	tests, err := os.ReadFile("testdata/zod_test.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := data["zod.gen.go"]; !ok {
		t.Fatalf("expected zod.gen.go, got %v", slices.Sorted(maps.Keys(data)))
	}
	dir := t.TempDir()
	writeFixture(t, dir, data, tests)
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Zod tests: %v\n%s", err, output)
	}
}
