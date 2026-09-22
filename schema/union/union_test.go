package union

import (
	"encoding/json/jsontext"
	"testing"
)

type form struct {
	Mode string `json:"mode"`
	Name string `json:"name,omitzero"`
}

func TestAsAndNew(t *testing.T) {
	rules := Table(
		Alt[string](Rule{NonNull: true, Literal: jsontext.Value(`"yes"`)}, Rule{NonNull: true, Literal: jsontext.Value(`"no"`)}),
		Alt[float64](Rule{NonNull: true}),
		Alt[jsontext.Value](Rule{Null: true}),
		Alt[form](Rule{NonNull: true, Required: []string{"mode"}, Tags: []Tag{{"mode", jsontext.Value(`"form"`)}}}),
	)
	if v, err := As[string]("u", rules, jsontext.Value(`"no"`)); err != nil || v != "no" {
		t.Fatalf("second literal: %v %q", err, v)
	}
	if _, err := As[string]("u", rules, jsontext.Value(`"maybe"`)); err == nil {
		t.Fatal("unlisted literal accepted")
	}
	if _, err := As[float64]("u", rules, jsontext.Value(`null`)); err == nil {
		t.Fatal("null accepted as number")
	}
	if v, err := As[jsontext.Value]("u", rules, jsontext.Value(` null `)); err != nil || v.Kind() != 'n' {
		t.Fatalf("null alternative: %v %s", err, v)
	}
	if _, err := As[form]("u", rules, jsontext.Value(`{"mode":"other"}`)); err == nil {
		t.Fatal("wrong tag accepted")
	}
	raw, err := New("u", rules, form{Name: "x"})
	if err != nil || string(raw) != `{"mode":"form","name":"x"}` {
		t.Fatalf("tag not spliced: %v %s", err, raw)
	}
	if raw, err := New("u", rules, form{Mode: "form"}); err != nil || string(raw) != `{"mode":"form"}` {
		t.Fatalf("matching value re-encoded: %v %s", err, raw)
	}
	if _, err := New("u", rules, "maybe"); err == nil {
		t.Fatal("constructor accepted a value matching no literal")
	}
}
