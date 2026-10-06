// Command jokes-mcp serves the joke tools over MCP on stdio.
//
// Register it with Claude Code:
//
//	claude mcp add jokes -- go run ./cmd/jokes-mcp
//
// or build it first (make build) and point at bin/jokes-mcp. Use -trace to log
// every JSON-RPC message to stderr (stdout is reserved for the protocol).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/AriT93/agent-lab/internal/dadjoke"
	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/stage4"
	"github.com/AriT93/agent-lab/internal/trace"
)

func main() {
	showTrace := flag.Bool("trace", false, "log every JSON-RPC message to stderr")
	flag.Parse()

	s := stage4.NewServer(jokeapi.New(), dadjoke.New())
	if *showTrace {
		s.Trace = trace.New(os.Stderr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := s.Serve(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "jokes-mcp:", err)
		os.Exit(1)
	}
}
