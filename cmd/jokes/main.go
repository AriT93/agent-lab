// Command jokes is a REPL for trying each stage of the agent lab.
//
//	go run ./cmd/jokes -stage 3 -trace
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/AriT93/agent-lab/internal/dadjoke"
	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/ollama"
	"github.com/AriT93/agent-lab/internal/stage0"
	"github.com/AriT93/agent-lab/internal/stage1"
	"github.com/AriT93/agent-lab/internal/stage2"
	"github.com/AriT93/agent-lab/internal/stage3"
	"github.com/AriT93/agent-lab/internal/trace"
)

// Interpreter turns what the user typed into a JokeAPI request (stages 0–1).
type Interpreter interface {
	Interpret(ctx context.Context, text string) (jokeapi.Request, error)
}

// respondFunc answers one user message. Stages 0–1 fetch the joke themselves;
// from stage 2 on, the agent does it.
type respondFunc func(ctx context.Context, text string) (string, error)

func main() {
	stage := flag.Int("stage", 3, "0 = keywords, 1 = structured output, 2 = tool-calling agent, 3 = multi-tool agent with memory")
	backend := flag.String("backend", "ollama", `LLM backend for stage 1: "ollama" or "claude" (stages 2–3 are ollama only)`)
	model := flag.String("model", "", "model name (default: "+ollama.DefaultModel+" or "+stage1.DefaultClaudeModel+")")
	numCtx := flag.Int("ctx", 8192, "ollama: context window in tokens; bigger costs memory")
	keepAlive := flag.String("keep-alive", "2m", `ollama: how long the model stays loaded when idle ("0s" unloads after each call)`)
	think := flag.Bool("think", false, "stages 2–3: let the model reason before acting (slower, often smarter)")
	showTrace := flag.Bool("trace", false, "print each step: prompts, model output, tool calls, API requests")
	flag.Parse()

	var tr *trace.Tracer
	if *showTrace {
		tr = trace.New(os.Stderr)
	}

	jokes := jokeapi.New()
	llm := ollama.New()
	llm.NumCtx, llm.KeepAlive = *numCtx, *keepAlive
	if *model != "" {
		llm.Model = *model
	}
	usesOllama := *stage >= 2 || (*stage == 1 && *backend == "ollama")
	var reset func() // stages with memory can forget the conversation

	var respond respondFunc
	switch {
	case *stage == 0:
		respond = fetchWith(stage0.Interpreter{}, jokes, tr)
	case *stage == 1 && *backend == "ollama":
		respond = fetchWith(&stage1.Ollama{Client: llm, Trace: tr}, jokes, tr)
	case *stage == 1 && *backend == "claude":
		c := stage1.NewClaude()
		c.Trace = tr
		if *model != "" {
			c.Model = *model
		}
		respond = fetchWith(c, jokes, tr)
	case *stage == 2:
		a := stage2.New(llm, jokes)
		a.Trace, a.Think = tr, *think
		respond = a.Respond
	case *stage == 3:
		a := stage3.New(llm, jokes, dadjoke.New())
		a.Trace, a.Think = tr, *think
		respond, reset = a.Respond, a.Reset
	default:
		fmt.Fprintf(os.Stderr, "unsupported -stage %d / -backend %q\n", *stage, *backend)
		os.Exit(2)
	}

	// Free the model's memory on the way out, including on Ctrl-C, instead of
	// leaving it resident until keep-alive expires.
	unload := func() {
		if usesOllama {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			llm.Unload(ctx)
		}
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	go func() {
		<-sigs
		fmt.Println()
		unload()
		os.Exit(130)
	}()
	defer unload()

	label := fmt.Sprintf("stage %d", *stage)
	if usesOllama {
		label += fmt.Sprintf(" · %s · ctx %d · keep-alive %s", llm.Model, llm.NumCtx, llm.KeepAlive)
	}
	fmt.Printf("agent-lab %s\nAsk for a joke; blank line or Ctrl-D to quit.\n", label)
	if reset != nil {
		fmt.Println("This stage remembers the conversation; /reset forgets it.")
	}

	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !in.Scan() {
			break
		}
		text := strings.TrimSpace(in.Text())
		if text == "" || text == "quit" || text == "exit" {
			break
		}
		if text == "/reset" {
			if reset != nil {
				reset()
				fmt.Println("(conversation forgotten)")
			}
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		reply, err := respond(ctx, text)
		cancel()
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		fmt.Println(reply)
	}
}

func fetchWith(interp Interpreter, jokes *jokeapi.Client, tr *trace.Tracer) respondFunc {
	return func(ctx context.Context, text string) (string, error) {
		req, err := interp.Interpret(ctx, text)
		if err != nil {
			return "", fmt.Errorf("interpreting request: %w", err)
		}
		tr.Step("interpreted request", req)
		tr.Step("GET", req.URL(jokes.BaseURL))

		j, err := jokes.Fetch(ctx, req)
		if err != nil {
			return "", err
		}
		return j.Text(), nil
	}
}
