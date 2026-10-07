// Command jokes-eval scores a stage against the eval tables in internal/evals.
//
//	go run ./cmd/jokes-eval -stage 0              # keyword baseline, no LLM
//	go run ./cmd/jokes-eval -stage 1 -n 3         # structured output, 3 runs per case
//	go run ./cmd/jokes-eval -stage 3 -run restr   # agent conversations matching "restr"
//	go run ./cmd/jokes-eval -stage 6 -provider claude   # same conversations, another model
//
// Agent evals (stages 3 and 6) fetch real jokes, so they hit JokeAPI and icanhazdadjoke.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/AriT93/agent-lab/internal/dadjoke"
	"github.com/AriT93/agent-lab/internal/evals"
	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/ollama"
	"github.com/AriT93/agent-lab/internal/provider"
	"github.com/AriT93/agent-lab/internal/stage0"
	"github.com/AriT93/agent-lab/internal/stage1"
	"github.com/AriT93/agent-lab/internal/stage3"
	"github.com/AriT93/agent-lab/internal/stage3lc"
	"github.com/AriT93/agent-lab/internal/stage4"
	"github.com/AriT93/agent-lab/internal/stage6"
	"github.com/AriT93/agent-lab/internal/trace"
)

func main() {
	stage := flag.String("stage", "0", "0 = keywords, 1 = structured output, 3 = multi-tool agent, 3b = stage 3 in langchaingo, 6 = agent on any provider")
	providerName := flag.String("provider", "ollama", "stage 6: "+provider.Names)
	backend := flag.String("backend", "ollama", `stage 1 backend: "ollama" or "claude"`)
	model := flag.String("model", "", "model name (default: the backend's default)")
	runs := flag.Int("n", 1, "times to run each case; model output varies, so a pass rate beats one run")
	only := flag.String("run", "", "only run cases whose name matches this regexp")
	numCtx := flag.Int("ctx", 8192, "ollama: context window in tokens")
	showTrace := flag.Bool("trace", false, "print each step of every case")
	flag.Parse()

	var filter *regexp.Regexp
	if *only != "" {
		var err error
		if filter, err = regexp.Compile(*only); err != nil {
			fmt.Fprintln(os.Stderr, "bad -run:", err)
			os.Exit(2)
		}
	}
	var tr *trace.Tracer
	if *showTrace {
		tr = trace.New(os.Stderr)
	}

	llm := ollama.New()
	llm.NumCtx = *numCtx
	if *model != "" {
		llm.Model = *model
	}
	defer func() {
		if *stage == "3" || *stage == "3b" || (*stage == "1" && *backend == "ollama") || (*stage == "6" && *providerName == "ollama") {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			llm.Unload(ctx)
		}
	}()

	var runOnce func(context.Context) []evals.Result
	switch {
	case *stage == "0":
		runOnce = func(ctx context.Context) []evals.Result {
			return evals.RunRequests(ctx, stage0.Interpreter{}, evals.RequestCases, filter)
		}
	case *stage == "1" && *backend == "ollama":
		in := &stage1.Ollama{Client: llm, Trace: tr}
		runOnce = func(ctx context.Context) []evals.Result {
			return evals.RunRequests(ctx, in, evals.RequestCases, filter)
		}
	case *stage == "1" && *backend == "claude":
		in := stage1.NewClaude()
		in.Trace = tr
		if *model != "" {
			in.Model = *model
		}
		runOnce = func(ctx context.Context) []evals.Result {
			return evals.RunRequests(ctx, in, evals.RequestCases, filter)
		}
	case *stage == "6":
		p, err := provider.ByName(*providerName, *model, llm, false)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		jokes, dads := jokeapi.New(), dadjoke.New()
		newAgent := func(record func(evals.Call)) evals.Agent {
			a := stage6.New(p, stage4.NewServer(jokes, dads).Tools)
			a.Trace = tr
			if *providerName == "ollama" {
				a.TokenBudget = llm.NumCtx * 3 / 4
			}
			a.OnTool = func(name string, args json.RawMessage, result string) {
				record(evals.Call{Tool: name, Args: string(args), Result: result})
			}
			return a
		}
		runOnce = func(ctx context.Context) []evals.Result {
			return evals.RunConversations(ctx, newAgent, evals.ConversationCases, filter)
		}
	case *stage == "3b":
		jokes, dads := jokeapi.New(), dadjoke.New()
		newAgent := func(record func(evals.Call)) evals.Agent {
			// Over /v1 the context size and keep-alive cannot be set (see stage3lc).
			a, err := stage3lc.New(stage3lc.Config{BaseURL: llm.BaseURL + "/v1", Model: llm.Model, MaxTokens: llm.NumPredict}, jokes, dads)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			a.Trace = tr
			a.OnTool = func(name, input, result string) {
				record(evals.Call{Tool: name, Args: input, Result: result})
			}
			return a
		}
		runOnce = func(ctx context.Context) []evals.Result {
			return evals.RunConversations(ctx, newAgent, evals.ConversationCases, filter)
		}
	case *stage == "3":
		jokes, dads := jokeapi.New(), dadjoke.New()
		newAgent := func(record func(evals.Call)) evals.Agent {
			a := stage3.New(llm, jokes, dads)
			a.Trace = tr
			a.OnTool = func(name string, args json.RawMessage, result string) {
				record(evals.Call{Tool: name, Args: string(args), Result: result})
			}
			return a
		}
		runOnce = func(ctx context.Context) []evals.Result {
			return evals.RunConversations(ctx, newAgent, evals.ConversationCases, filter)
		}
	default:
		fmt.Fprintf(os.Stderr, "unsupported -stage %q / -backend %q\n", *stage, *backend)
		os.Exit(2)
	}

	var all [][]evals.Result
	for i := 1; i <= *runs; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		res := runOnce(ctx)
		cancel()
		if *runs > 1 {
			fmt.Printf("── run %d/%d\n", i, *runs)
		}
		evals.Report(os.Stdout, res)
		all = append(all, res)
	}
	if *runs > 1 {
		names, passes := evals.Tally(all)
		fmt.Printf("\n── pass rate over %d runs\n", *runs)
		for _, n := range names {
			fmt.Printf("%3d/%d  %s\n", passes[n], *runs, n)
		}
	}
}
