package acp1

import (
	"iter"
	"strings"

	schema "github.com/ironpark/acp-go/schema/v1"
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

// Texts yields the text of each text block in blocks, skipping other kinds.
func Texts(blocks []ContentBlock) iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, block := range blocks {
			if text, ok := TextOf(block); ok && !yield(text) {
				return
			}
		}
	}
}

// JoinTexts concatenates the text blocks in blocks, skipping other kinds,
// such as a prompt's text:
//
//	prompt := acp1.JoinTexts(params.Prompt)
func JoinTexts(blocks []ContentBlock) string {
	var b strings.Builder
	for text := range Texts(blocks) {
		b.WriteString(text)
	}
	return b.String()
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

// LineRange returns the lines of a file's content that a fs/read_text_file
// request asks for: at most limit lines from the 1-based line, each with its
// line ending. A nil or zero line starts at the first line and a nil limit
// reads to the end:
//
//	return &acp1.ReadTextFileResponse{Content: acp1.LineRange(string(data), params.Line, params.Limit)}, nil
func LineRange(content string, line, limit *uint32) string {
	if line != nil {
		for n := *line; n > 1; n-- {
			i := strings.IndexByte(content, '\n')
			if i < 0 {
				return ""
			}
			content = content[i+1:]
		}
	}
	if limit == nil {
		return content
	}
	end := 0
	for range *limit {
		i := strings.IndexByte(content[end:], '\n')
		if i < 0 {
			return content
		}
		end += i + 1
	}
	return content[:end]
}
