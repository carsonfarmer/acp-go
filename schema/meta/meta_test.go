package meta

import (
	"encoding/json/v2"
	"testing"
)

func TestSetGet(t *testing.T) {
	var m Meta
	if err := m.Set("trace", map[string]int{"depth": 2}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := m.Get[map[string]int]("trace")
	if err != nil || !ok || got["depth"] != 2 {
		t.Fatalf("got %v %v %v", got, ok, err)
	}
	if _, ok, err := m.Get[string]("missing"); ok || err != nil {
		t.Fatalf("missing key: %v %v", ok, err)
	}
	if _, _, err := m.Get[string]("trace"); err == nil {
		t.Fatal("decoded an object into a string")
	}
}

func TestOfKeepsLargeNumbers(t *testing.T) {
	m, err := Of(map[string]any{"id": int64(9007199254740993)})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(m)
	if string(out) != `{"id":9007199254740993}` {
		t.Fatalf("got %s", out)
	}
}
