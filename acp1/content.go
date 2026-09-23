package acp1

import (
	"iter"

	schema "github.com/ironpark/go-acp/schema/v1"
)

// TextBlock wraps text as a content block, the usual shape of a prompt or a
// message chunk.
func TextBlock(text string) ContentBlock {
	return schema.NewContentBlock(schema.ContentBlockText{Text: text})
}

// TextOf returns the text of a text content block, or false for any other kind.
func TextOf(block ContentBlock) (string, bool) {
	text, ok := block.As[schema.ContentBlockText]()
	return text.Text, ok
}

// Texts yields the text of each text block in blocks, skipping other kinds:
//
//	prompt := strings.Join(slices.Collect(acp1.Texts(params.Prompt)), "")
func Texts(blocks []ContentBlock) iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, block := range blocks {
			if text, ok := TextOf(block); ok && !yield(text) {
				return
			}
		}
	}
}

// ToolText wraps text as tool call output.
func ToolText(text string) ToolCallContent {
	return schema.NewToolCallContent(schema.ToolCallContentContent{Content: TextBlock(text)})
}

// ToolDiff reports a file change as tool call output: path's content goes
// from oldText, or nothing for a new file, to newText.
func ToolDiff(path string, oldText *string, newText string) ToolCallContent {
	return schema.NewToolCallContent(schema.ToolCallContentDiff{Path: path, OldText: oldText, NewText: newText})
}

// ToolTerminal embeds a terminal's live output in a tool call, for a command
// started with [AgentSideConnection.NewTerminal].
func ToolTerminal(id TerminalID) ToolCallContent {
	return schema.NewToolCallContent(schema.ToolCallContentTerminal{TerminalID: id})
}
