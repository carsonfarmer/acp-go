// Command dual-agent serves the echo agent in both ACP v1 and the draft v2
// from one binary.
//
// router.ProtocolRouter reads the client's initialize request and hands the
// connection to the highest version both sides support. Each version has its
// own façade package and its own agent type, in v1.go and v2.go; they share
// nothing but the router. Try it with the dual-client example, which prefers v2, and the
// client example, which speaks v1.
package main

import (
	"context"
	"log"
	"os"

	"github.com/ironpark/acp-go/acp1"
	"github.com/ironpark/acp-go/acp2"
	"github.com/ironpark/acp-go/router"
)

var info = struct{ name, version string }{"dual-agent", "0.1.0"}

func main() {
	r := router.New().
		WithV1(func(c *acp1.AgentSideConnection) acp1.Agent { return &v1Agent{client: c} }).
		WithV2(func(c *acp2.AgentSideConnection) acp2.Agent { return newV2Agent(c) })
	if err := r.ServeStdio(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
