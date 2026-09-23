package acp2_test

import (
	"testing"

	"github.com/ironpark/acp-go/acp2"
	schema "github.com/ironpark/acp-go/schema/v2"
)

func TestJoinTexts(t *testing.T) {
	blocks := []acp2.ContentBlock{
		acp2.TextBlock("a"),
		acp2.NewContentBlock(schema.ContentBlockImage{Data: "x", MIMEType: "image/png"}),
		acp2.TextBlock("b"),
	}
	if got := acp2.JoinTexts(blocks); got != "ab" {
		t.Fatalf("JoinTexts = %q, want %q", got, "ab")
	}
}
