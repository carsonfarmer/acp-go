package acp1_test

import (
	"bufio"
	"encoding/json/v2"
	"io"
	"testing"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
)

// A request whose params fail strict decoding, such as the lone surrogate
// JSON.stringify sends for a string cut mid-character, is answered with
// invalid params instead of dropped with the client left waiting.
func TestUndecodableParamsAreAnswered(t *testing.T) {
	toAgentR, toAgentW := io.Pipe()
	toClientR, toClientW := io.Pipe()
	t.Cleanup(func() { toAgentR.Close(); toClientR.Close() })
	conn := acp1.NewAgentSideConnection(func(*acp1.AgentSideConnection) acp1.Agent { return bareAgent{} },
		acp.NewStdioTransport(toAgentR, toClientW))
	go conn.Start(t.Context())

	for id, message := range []string{
		`{"jsonrpc":"2.0","id":0,"method":"session/prompt","params":{"sessionId":"s","prompt":[{"type":"text","text":"\ud83d"}]}}`,
		"{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"session/prompt\",\"params\":{\"sessionId\":\"s\",\"prompt\":[{\"type\":\"text\",\"text\":\"\xff\"}]}}",
		`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"s","sessionId":"t","prompt":[]}}`,
	} {
		go toAgentW.Write([]byte(message + "\n"))
		line := make(chan []byte, 1)
		go func() {
			data, _ := bufio.NewReader(toClientR).ReadBytes('\n')
			line <- data
		}()
		var response struct {
			ID    int `json:"id"`
			Error struct {
				Code acp.ErrorCode `json:"code"`
			} `json:"error"`
		}
		select {
		case data := <-line:
			if err := json.Unmarshal(data, &response); err != nil {
				t.Fatalf("response %s: %v", data, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("request %d was never answered", id)
		}
		if response.ID != id || response.Error.Code != acp.ErrorCodeInvalidParams {
			t.Errorf("request %d answered %+v, want invalid params", id, response)
		}
	}
}
