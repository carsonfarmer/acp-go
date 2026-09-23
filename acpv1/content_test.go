package acpv1_test

import (
	"slices"
	"testing"

	"github.com/ironpark/go-acp/acpv1"
	schema "github.com/ironpark/go-acp/schema/v1"
)

func TestTexts(t *testing.T) {
	blocks := []acpv1.ContentBlock{
		acpv1.TextBlock("a"),
		schema.NewContentBlock(schema.ContentBlockImage{Data: "…", MIMEType: "image/png"}),
		acpv1.TextBlock("b"),
		acpv1.TextBlock("c"),
	}
	if got := slices.Collect(acpv1.Texts(blocks)); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("got %q", got)
	}
	for text := range acpv1.Texts(blocks) {
		if text != "a" {
			t.Fatalf("iteration continued past break: %q", text)
		}
		break
	}
}
