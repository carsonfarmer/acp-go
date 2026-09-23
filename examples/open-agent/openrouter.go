package main

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// openRouter is a minimal streaming client for OpenRouter's OpenAI-compatible
// Chat Completions API, written against net/http so the example needs no
// dependencies. Any OpenAI-compatible endpoint works through baseURL.
type openRouter struct {
	baseURL string
	apiKey  string
	model   string

	// contextLength is the model's context window in tokens, from /models, or
	// 0 when unknown, which turns usage reporting off.
	contextLength int
}

// message is one entry of a chat conversation.
type message struct {
	Role       string     `json:"role"` // system, user, assistant or tool
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitzero"`
	ToolCallID string     `json:"tool_call_id,omitzero"` // the call a tool message answers

	// Reasoning models need their reasoning sent back with the assistant
	// message that called a tool, or the next request can fail. The details
	// are kept as the model produced them; the plain text only when there are
	// none.
	Reasoning        string            `json:"reasoning,omitzero"`
	ReasoningDetails []reasoningDetail `json:"reasoning_details,omitzero"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON text, streamed in fragments
}

// reasoningDetail is one block of a model's reasoning: plain text, a summary
// or an encrypted blob, depending on the model.
type reasoningDetail struct {
	Type      string `json:"type"`
	ID        string `json:"id,omitzero"`
	Format    string `json:"format,omitzero"`
	Index     int    `json:"index"`
	Text      string `json:"text,omitzero"`
	Summary   string `json:"summary,omitzero"`
	Data      string `json:"data,omitzero"`
	Signature string `json:"signature,omitzero"`
}

// tool describes a function the model may call.
type tool struct {
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  jsontext.Value `json:"parameters"` // JSON Schema
}

type usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Cost             float64 `json:"cost"` // in USD; OpenRouter only
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	Tools    []tool    `json:"tools,omitzero"`
	Stream   bool      `json:"stream"`
}

// chunk is one server-sent event of a streamed completion. OpenRouter sends
// usage in the last one without being asked.
type chunk struct {
	Choices []struct {
		Delta struct {
			Content          string            `json:"content"`
			Reasoning        string            `json:"reasoning"`
			ReasoningDetails []reasoningDetail `json:"reasoning_details"`
			ToolCalls        []struct {
				Index    int          `json:"index"`
				ID       string       `json:"id"`
				Function functionCall `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// completion is a finished model response.
type completion struct {
	message      message
	finishReason string // stop, tool_calls, length, content_filter or error
	usage        *usage // nil if the endpoint sent none
}

var errUnknownModel = errors.New("unknown model")

// lookupModel reads the model's context length from /models. It fails with
// errUnknownModel if the endpoint does not list the model.
func (c *openRouter) lookupModel(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var models struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	if err := json.UnmarshalRead(resp.Body, &models); err != nil {
		return fmt.Errorf("reading models: %w", err)
	}
	for _, m := range models.Data {
		if m.ID == c.model {
			c.contextLength = m.ContextLength
			return nil
		}
	}
	return fmt.Errorf("%w %q", errUnknownModel, c.model)
}

// stream sends the conversation and streams the response: text to onText,
// reasoning to onReasoning. Tool calls arrive in the returned message.
// Cancelling ctx aborts the request.
func (c *openRouter) stream(ctx context.Context, messages []message, tools []tool, onText, onReasoning func(string) error) (*completion, error) {
	body, err := json.Marshal(chatRequest{Model: c.model, Messages: messages, Tools: tools, Stream: true})
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/chat/completions", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var (
		result    = &completion{message: message{Role: "assistant"}}
		content   strings.Builder
		reasoning strings.Builder
		details   []reasoningDetail
		calls     []toolCall
	)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		// Blank lines end events, and lines starting with ":" are comments,
		// such as OpenRouter's ": OPENROUTER PROCESSING" keep-alive.
		data, ok := bytes.CutPrefix(scanner.Bytes(), []byte("data:"))
		if !ok {
			continue
		}
		data = bytes.TrimSpace(data)
		if string(data) == "[DONE]" {
			break
		}
		var event chunk
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, fmt.Errorf("reading stream: %w", err)
		}
		if event.Error != nil {
			return nil, fmt.Errorf("model error: %s", event.Error.Message)
		}
		if event.Usage != nil {
			result.usage = event.Usage
		}
		if len(event.Choices) == 0 {
			continue
		}
		choice := event.Choices[0]
		delta := choice.Delta
		if delta.Reasoning != "" {
			reasoning.WriteString(delta.Reasoning)
			if err := onReasoning(delta.Reasoning); err != nil {
				return nil, err
			}
		}
		for _, detail := range delta.ReasoningDetails {
			details = mergeReasoning(details, detail)
		}
		if delta.Content != "" {
			content.WriteString(delta.Content)
			if err := onText(delta.Content); err != nil {
				return nil, err
			}
		}
		// A tool call streams in pieces sharing an index: the first carries
		// its id and name, the rest fragments of its arguments.
		for _, part := range delta.ToolCalls {
			for len(calls) <= part.Index {
				calls = append(calls, toolCall{Type: "function"})
			}
			call := &calls[part.Index]
			call.ID = cmp.Or(part.ID, call.ID)
			call.Function.Name = cmp.Or(part.Function.Name, call.Function.Name)
			call.Function.Arguments += part.Function.Arguments
		}
		if choice.FinishReason != "" {
			result.finishReason = choice.FinishReason
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	result.message.Content = content.String()
	result.message.ToolCalls = calls
	result.message.ReasoningDetails = details
	if len(details) == 0 {
		result.message.Reasoning = reasoning.String()
	}
	return result, nil
}

// mergeReasoning adds a streamed reasoning fragment to the block with the same
// index and type, or starts a new block.
func mergeReasoning(details []reasoningDetail, part reasoningDetail) []reasoningDetail {
	for i := range details {
		d := &details[i]
		if d.Index != part.Index || (part.Type != "" && part.Type != d.Type) {
			continue
		}
		d.Type = cmp.Or(part.Type, d.Type)
		d.ID = cmp.Or(part.ID, d.ID)
		d.Format = cmp.Or(part.Format, d.Format)
		d.Signature = cmp.Or(part.Signature, d.Signature)
		d.Text += part.Text
		d.Summary += part.Summary
		d.Data += part.Data
		return details
	}
	return append(details, part)
}

// do sends an authenticated request and turns a non-2xx response into an
// error carrying the response body, where OpenRouter explains the failure.
func (c *openRouter) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	// Optional headers that attribute the requests to this app on OpenRouter.
	req.Header.Set("HTTP-Referer", "https://github.com/ironpark/go-acp")
	req.Header.Set("X-Title", "go-acp open-agent")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		text, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, bytes.TrimSpace(text))
	}
	return resp, nil
}
