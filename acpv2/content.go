package acpv2

import schema "github.com/ironpark/go-acp/schema/v2"

// TextBlock wraps text as a content block, the usual shape of a prompt or a
// message chunk.
func TextBlock(text string) ContentBlock {
	return schema.NewContentBlock(schema.ContentBlockText{Text: text})
}

// TextOf returns the text of a text content block, or false for any other kind.
func TextOf(block ContentBlock) (string, bool) {
	text, ok := block.Variant().(schema.ContentBlockText)
	return text.Text, ok
}

// ToolText wraps text as tool call output.
func ToolText(text string) ToolCallContent {
	return schema.NewToolCallContent(schema.ToolCallContentContent{Content: TextBlock(text)})
}
