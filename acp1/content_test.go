package acp1_test

import (
	"encoding/json/v2"
	"slices"
	"testing"

	"github.com/ironpark/acp-go/acp1"
	schema "github.com/ironpark/acp-go/schema/v1"
)

func TestTexts(t *testing.T) {
	blocks := []acp1.ContentBlock{
		acp1.TextBlock("a"),
		schema.NewContentBlock(schema.ContentBlockImage{Data: "…", MIMEType: "image/png"}),
		acp1.TextBlock("b"),
		acp1.TextBlock("c"),
	}
	if got := slices.Collect(acp1.Texts(blocks)); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("got %q", got)
	}
	for text := range acp1.Texts(blocks) {
		if text != "a" {
			t.Fatalf("iteration continued past break: %q", text)
		}
		break
	}
}

func TestToolContentHelpers(t *testing.T) {
	for _, tt := range []struct {
		content acp1.ToolCallContent
		want    string
	}{
		{acp1.ToolDiff("/a.txt", new("old"), "new"), `{"type":"diff","path":"/a.txt","oldText":"old","newText":"new"}`},
		{acp1.ToolDiff("/b.txt", nil, "created"), `{"type":"diff","path":"/b.txt","newText":"created"}`},
		{acp1.ToolTerminal("term_1"), `{"type":"terminal","terminalId":"term_1"}`},
	} {
		got, err := json.Marshal(tt.content)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tt.want {
			t.Errorf("got %s, want %s", got, tt.want)
		}
	}
}

func TestJoinTexts(t *testing.T) {
	blocks := []acp1.ContentBlock{
		acp1.TextBlock("a"),
		acp1.NewContentBlock(schema.ContentBlockImage{Data: "x", MIMEType: "image/png"}),
		acp1.TextBlock("b"),
	}
	if got := acp1.JoinTexts(blocks); got != "ab" {
		t.Fatalf("JoinTexts = %q, want %q", got, "ab")
	}
}

func TestLineRange(t *testing.T) {
	const lf, crlf = "a\nb\nc\n", "a\r\nb\r\nc"
	for _, tc := range []struct {
		content     string
		line, limit *uint32
		want        string
	}{
		{lf, nil, nil, lf},
		{lf, new(uint32(0)), nil, lf}, // the schema allows 0; it reads from the first line
		{lf, new(uint32(2)), nil, "b\nc\n"},
		{lf, new(uint32(2)), new(uint32(1)), "b\n"},
		{lf, nil, new(uint32(2)), "a\nb\n"},
		{lf, new(uint32(3)), new(uint32(5)), "c\n"},
		{lf, new(uint32(4)), nil, ""},
		{lf, nil, new(uint32(0)), ""},
		{crlf, new(uint32(2)), nil, "b\r\nc"}, // line endings are kept as the file has them
		{crlf, new(uint32(3)), new(uint32(1)), "c"},
	} {
		var line uint32
		if tc.line != nil {
			line = *tc.line
		}
		if got := acp1.LineRange(tc.content, tc.line, tc.limit); got != tc.want {
			t.Errorf("LineRange(%q, %v, %v) = %q, want %q", tc.content, tc.line, tc.limit, got, tc.want)
		}
		if tc.line != nil && *tc.line != line {
			t.Errorf("LineRange changed the request's line from %d to %d", line, *tc.line)
		}
	}
}
