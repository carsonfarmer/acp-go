package jsonrpc

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"testing"
)

func TestErrorConstructors(t *testing.T) {
	if got := InvalidParams("unknown mode").Message; got != "Invalid params: unknown mode" {
		t.Errorf("detail message = %q", got)
	}
	if got := InvalidParams("").Message; got != "Invalid params" {
		t.Errorf("empty detail message = %q", got)
	}
	if err := ResourceNotFound("terminal t1"); err.Data != nil {
		t.Errorf("detail became data: %v", err.Data)
	}
	base := ResourceNotFound("/a")
	withData := base.WithData(map[string]string{"uri": "/a"})
	if base.Data != nil || withData.Data == nil || withData.Message != base.Message || withData.Code != base.Code {
		t.Errorf("WithData = %+v from %+v; want a copy with data", withData, base)
	}
}

func TestRequestErrorIs(t *testing.T) {
	sentinel := InvalidRequest("session busy")
	// The peer's copy of the sentinel is a different value with the same code
	// and message.
	received := &RequestError{Code: CodeInvalidRequest, Message: "Invalid request: session busy", Data: jsontext.Value(`{}`)}
	if !errors.Is(received, sentinel) {
		t.Error("the peer's copy does not match the sentinel")
	}
	if errors.Is(InvalidRequest("other"), sentinel) {
		t.Error("a different message matches")
	}
	if !errors.Is(fmt.Errorf("wrapped: %w", InvalidParams("x")), &RequestError{Code: CodeInvalidParams}) {
		t.Error("a code-only target does not match")
	}
	if errors.Is(InvalidParams("x"), &RequestError{Code: CodeInternalError}) {
		t.Error("a different code matches")
	}
}
