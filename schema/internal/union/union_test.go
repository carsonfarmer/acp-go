package union

import (
	"bytes"
	"encoding/json/jsontext"
	"strings"
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
	if _, err := New("u", rules, form{Name: "x"}); err == nil {
		t.Fatal("constructor accepted an object missing its tag")
	}
	if raw, err := New("u", rules, form{Mode: "form", Name: "x"}); err != nil || string(raw) != `{"mode":"form","name":"x"}` {
		t.Fatalf("tagged value: %v %s", err, raw)
	}
	if _, err := New("u", rules, "maybe"); err == nil {
		t.Fatal("constructor accepted a value matching no literal")
	}
}

type payloadT struct {
	Text string `json:"text"`
}

func TestSpliceTag(t *testing.T) {
	var buf bytes.Buffer
	enc := jsontext.NewEncoder(&buf)
	if err := SpliceTag(enc, "type", "text", payloadT{Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != `{"type":"text","text":"hi"}` {
		t.Fatalf("spliced: %s", got)
	}
	var out payloadT
	if err := UnspliceTag(jsontext.NewDecoder(strings.NewReader(buf.String())), "type", "text", &out); err != nil || out.Text != "hi" {
		t.Fatalf("unspliced: %v %+v", err, out)
	}
	if err := UnspliceTag(jsontext.NewDecoder(strings.NewReader(`{"type":"image"}`)), "type", "text", &out); err == nil {
		t.Fatal("wrong discriminator accepted")
	}
}

func TestReadTag(t *testing.T) {
	cases := []struct {
		raw     string
		value   string
		present bool
		fails   bool
	}{
		{raw: `{"a":{"type":"x"},"type":"text","b":[1]}`, value: "text", present: true},
		{raw: `{"a":1}`},
		{raw: `{"type":null}`},
		{raw: `{"type":"text"}`, value: "text", present: true},
		{raw: `{"type":1}`, fails: true},
		{raw: `{"type":"a","type":"b"}`, fails: true},
		{raw: `[]`, fails: true},
		{raw: `{"type":"a",}`, fails: true},
	}
	for _, c := range cases {
		value, present, err := ReadTag(jsontext.Value(c.raw), "type")
		if (err != nil) != c.fails || value != c.value || present != c.present {
			t.Errorf("ReadTag(%s) = %q, %v, %v; want %q, %v, fails %v", c.raw, value, present, err, c.value, c.present, c.fails)
		}
	}
	value, _, err := ReadTag(jsontext.Value(`{"type":"a","type":"b"}`), "type", jsontext.AllowDuplicateNames(true))
	if err != nil || value != "b" {
		t.Errorf("duplicate names allowed: got %q, %v; want the last one", value, err)
	}
}
