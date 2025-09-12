package main

import "testing"

func TestToTitleCase(t *testing.T) {
	tests := []struct {
		name string
		input string
		want string
	}{
		{
			name:  "simple snake_case",
			input: "hello_world",
			want:  "HelloWorld",
		},
		{
			name:  "kebab-case",
			input: "hello-world",
			want:  "HelloWorld",
		},
		{
			name:  "mixed case",
			input: "hello_World-test",
			want:  "HelloWorldTest",
		},
		{
			name:  "single word",
			input: "hello",
			want:  "Hello",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "single character",
			input: "a",
			want:  "A",
		},
		{
			name:  "uppercase input",
			input: "HELLO_WORLD",
			want:  "HelloWorld",
		},
		{
			name:  "mixed separators",
			input: "hello_world-test_case",
			want:  "HelloWorldTestCase",
		},
		{
			name:  "multiple underscores",
			input: "hello__world",
			want:  "HelloWorld",
		},
		{
			name:  "trailing separator",
			input: "hello_world_",
			want:  "HelloWorld",
		},
		{
			name:  "leading separator",
			input: "_hello_world",
			want:  "HelloWorld",
		},
		// camelCase tests
		{
			name:  "camelCase rawOutput",
			input: "rawOutput",
			want:  "RawOutput",
		},
		{
			name:  "camelCase rawInput",
			input: "rawInput",
			want:  "RawInput",
		},
		{
			name:  "camelCase sessionId",
			input: "sessionId",
			want:  "SessionId",
		},
		{
			name:  "camelCase toolCallId",
			input: "toolCallId",
			want:  "ToolCallId",
		},
		{
			name:  "long camelCase",
			input: "someVeryLongCamelCaseString",
			want:  "SomeVeryLongCamelCaseString",
		},
		{
			name:  "already capitalized",
			input: "AlreadyCapitalized",
			want:  "AlreadyCapitalized",
		},
		{
			name:  "single lowercase word",
			input: "simple",
			want:  "Simple",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toTitleCase(tt.input); got != tt.want {
				t.Errorf("toTitleCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToMultiLineComment(t *testing.T) {
	tests := []struct {
		name string
		input string
		want string
	}{
		{
			name:  "single line",
			input: "Hello world",
			want:  "// Hello world\n",
		},
		{
			name:  "multi line",
			input: "Hello\nworld\ntest",
			want:  "// Hello\n// world\n// test\n",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "line with spaces",
			input: "Hello world\nSecond line",
			want:  "// Hello world\n// Second line\n",
		},
		{
			name:  "single newline",
			input: "\n",
			want:  "// \n// \n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toMultiLineComment(tt.input); got != tt.want {
				t.Errorf("toMultiLineComment(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}