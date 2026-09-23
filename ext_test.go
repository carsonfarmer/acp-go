package acp

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"testing"
)

type echoParams struct {
	Text string `json:"text"`
}

type echoResult struct {
	Echo string `json:"echo"`
}

// loopback answers ExtMethod by encoding params and passing them through a
// router, the way a connection would.
type loopback struct{ router *ExtRouter }

func (l loopback) ExtMethod(ctx context.Context, method string, params any) (jsontext.Value, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	result, err := l.router.ExtMethod(ctx, method, raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func TestExtRouter(t *testing.T) {
	var r ExtRouter
	r.HandleExt("_test/echo", func(_ context.Context, p *echoParams) (*echoResult, error) {
		return &echoResult{Echo: p.Text}, nil
	})
	var notified string
	r.OnExtNotification("_test/note", func(_ context.Context, p *echoParams) error {
		notified = p.Text
		return nil
	})
	ctx := context.Background()

	got, err := CallExt[echoResult](ctx, loopback{&r}, "_test/echo", echoParams{Text: "hi"})
	if err != nil || got.Echo != "hi" {
		t.Fatalf("got %+v %v", got, err)
	}

	var reqErr *RequestError
	if _, err := r.ExtMethod(ctx, "_test/missing", nil); !errors.As(err, &reqErr) || reqErr.Code != ErrorCodeMethodNotFound {
		t.Fatalf("missing method: %v", err)
	}
	if _, err := r.ExtMethod(ctx, "_test/echo", jsontext.Value(`{"text":7}`)); !errors.As(err, &reqErr) || reqErr.Code != ErrorCodeInvalidParams {
		t.Fatalf("bad params: %v", err)
	}

	if err := r.ExtNotification(ctx, "_test/note", jsontext.Value(`{"text":"n"}`)); err != nil || notified != "n" {
		t.Fatalf("notification: %q %v", notified, err)
	}
	if err := r.ExtNotification(ctx, "_test/unknown", nil); err != nil {
		t.Fatalf("unregistered notification should be ignored: %v", err)
	}
}
