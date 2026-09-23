package jsonrpc

import "testing"

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
