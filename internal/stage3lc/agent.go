// Package stage3lc is stage 3 again, built with langchaingo.
//
// Lesson: what a framework gives you, and what it hides. Compare this file
// with internal/stage3: the loop, the message history and the trimming are gone
// (agents.Executor and memory.ConversationWindowBuffer do them), but so is
// some control:
//
//   - Tools take ONE free-text string. langchaingo's OpenAI-functions agent
//     advertises every tool as {"__arg1": string}, so the typed jokeapi.Schema
//     is gone and the model must write JSON into a string. Parsing it back is
//     our job; stage 1's lesson (typed requests) has to be re-learned here.
//   - A Go error from a tool aborts the whole run (Executor.doAction returns
//     it). The convention in this repo is that tool errors go back to the
//     model as text, so the tools below never return one.
//   - Memory keeps only the user/assistant text of each turn, not the tool
//     calls and results, so "explain that joke" works from the final answer only.
//     Executor inputs must all be strings, so the history is flattened into
//     "Human: ... / AI: ..." text and pasted into the system prompt, instead of
//     being sent as real chat messages like stage 3 does.
//   - langchaingo's native Ollama client drops tool definitions, so we use its
//     OpenAI client against Ollama's OpenAI-compatible /v1 endpoint instead.
//   - Ollama's /v1 endpoint has no place for num_ctx or keep_alive, so this
//     stage can't apply the repo's memory limits: the server's own defaults
//     (or the model's Modelfile) decide the context size, and -ctx and
//     -keep-alive have no effect here. Only the output cap (max_tokens) is sent.
//   - Our -trace is wired in through a callbacks.Handler. Token counts are
//     available but only inside GenerationInfo, which we have to dig out.
package stage3lc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tmc/langchaingo/agents"
	"github.com/tmc/langchaingo/callbacks"
	"github.com/tmc/langchaingo/chains"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
	"github.com/tmc/langchaingo/memory"
	"github.com/tmc/langchaingo/schema"
	"github.com/tmc/langchaingo/tools"

	"github.com/AriT93/agent-lab/internal/dadjoke"
	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/trace"
)

const systemPrompt = `You are a joke assistant in an ongoing conversation. You get jokes only by calling tools; never write a joke yourself.

Choosing a tool:
- search_dad_jokes: clean dad jokes, searchable by one short word. Good for everyday topics
  (animals, food, sports) and when the user wants something family friendly.
- get_joke: JokeAPI. Good for programming, dark, pun, spooky or Christmas jokes, and when the
  user sets content limits. Turn "clean"/"family friendly" into a full blacklist.

If a tool finds nothing, try a shorter or related word, the other tool, or broader filters.
Never loosen content limits the user asked for, including ones from earlier in the
conversation. After 3 failed tool calls in a row, say you couldn't find one.

Use the conversation: "another one" means a new joke like the last request; questions about
a joke you told ("explain it", "why is that funny") need no tool.

When you tell a joke, give its text exactly as the tool returned it. If you changed the
request (other tool, broader filters), add one short sentence saying so.`

// Config points langchaingo at a model server.
type Config struct {
	BaseURL   string // OpenAI-compatible endpoint, e.g. http://localhost:11434/v1
	Model     string
	MaxTokens int // output cap per model call; 0 = server default
}

type Agent struct {
	Trace *trace.Tracer

	// OnTool, if set, sees every tool call: its name, the string the model wrote as
	// input, and the result (used by evals).
	OnTool func(name, input, result string)

	exec *agents.Executor
	mem  *memory.ConversationWindowBuffer
	seen map[string]bool

	maxTokens int
}

func New(cfg Config, jokes *jokeapi.Client, dads *dadjoke.Client) (*Agent, error) {
	a := &Agent{seen: map[string]bool{}, maxTokens: cfg.MaxTokens}
	h := &handler{agent: a}
	llm, err := openai.New(
		openai.WithBaseURL(cfg.BaseURL),
		openai.WithToken("ollama"), // required by the client, ignored by Ollama
		openai.WithModel(cfg.Model),
		openai.WithCallback(h), // the executor's handler never sees the model calls, only the LLM does
	)
	if err != nil {
		return nil, err
	}
	a.mem = memory.NewConversationWindowBuffer(10, memory.WithMemoryKey("chat_history"))

	agent := agents.NewOpenAIFunctionsAgent(legacyMaxTokens{llm},
		[]tools.Tool{recording{jokeAPITool{jokes, a.seen}, a}, recording{dadJokeTool{dads, a.seen}, a}},
		agents.NewOpenAIOption().WithSystemMessage(systemPrompt+"\n\nConversation so far:\n{{.chat_history}}"),
	)
	a.exec = agents.NewExecutor(agent,
		agents.WithMemory(a.mem),
		agents.WithMaxIterations(6),
		agents.WithCallbacksHandler(h),
	)
	return a, nil
}

// Reset forgets the conversation and the jokes already told.
func (a *Agent) Reset() {
	a.mem.Clear(context.Background())
	clear(a.seen)
}

func (a *Agent) Respond(ctx context.Context, text string) (string, error) {
	var opts []chains.ChainCallOption
	if a.maxTokens > 0 {
		opts = append(opts, chains.WithMaxTokens(a.maxTokens))
	}
	out, err := chains.Run(ctx, a.exec, text, opts...)
	if err != nil {
		return "", err
	}
	return out, nil
}

// recording wraps a tool so evals can see each call. langchaingo's callbacks only
// report a tool's output, not which tool ran or what it was given.
type recording struct {
	tools.Tool
	agent *Agent
}

func (r recording) Call(ctx context.Context, input string) (string, error) {
	out, err := r.Tool.Call(ctx, input)
	if r.agent.OnTool != nil {
		r.agent.OnTool(r.Name(), input, out)
	}
	return out, err
}

// legacyMaxTokens makes the OpenAI client send max_tokens instead of its
// default max_completion_tokens, which servers that only imitate the OpenAI
// API may ignore. The option is per call, and the executor has no way to pass
// llms options through, so the only place to add it is around the model.
//
// It also repairs replayed tool calls. The tools take one string, which the
// functions agent wraps as {"__arg1": "..."} on the way in and unwraps on the
// way out, so on the next step it replays the bare string as the call's
// arguments. For search_dad_jokes' "random joke" that is "", which Ollama
// rejects with 400 "invalid tool call arguments". Re-wrap anything that isn't
// a JSON object.
type legacyMaxTokens struct{ llms.Model }

func (m legacyMaxTokens) GenerateContent(ctx context.Context, msgs []llms.MessageContent, opts ...llms.CallOption) (*llms.ContentResponse, error) {
	for _, msg := range msgs {
		for i, p := range msg.Parts {
			if tc, ok := p.(llms.ToolCall); ok && tc.FunctionCall != nil {
				var obj map[string]any
				if json.Unmarshal([]byte(tc.FunctionCall.Arguments), &obj) != nil {
					fc := *tc.FunctionCall
					fc.Arguments = toJSON(map[string]string{"__arg1": fc.Arguments})
					tc.FunctionCall = &fc
					msg.Parts[i] = tc
				}
			}
		}
	}
	return m.Model.GenerateContent(ctx, msgs, append(opts, openai.WithLegacyMaxTokensField())...)
}

// handler turns langchaingo callbacks into -trace steps.
type handler struct {
	callbacks.SimpleHandler
	agent *Agent
	step  int
}

func (h *handler) HandleLLMGenerateContentStart(_ context.Context, ms []llms.MessageContent) {
	h.step++
	var b strings.Builder
	for _, m := range ms {
		for _, p := range m.Parts {
			switch p := p.(type) {
			case llms.TextContent:
				fmt.Fprintf(&b, "[%s] %s\n", m.Role, p.Text)
			case llms.ToolCall:
				fmt.Fprintf(&b, "[%s] tool call %s(%s)\n", m.Role, p.FunctionCall.Name, p.FunctionCall.Arguments)
			case llms.ToolCallResponse:
				fmt.Fprintf(&b, "[%s] tool result %s\n", m.Role, p.Content)
			}
		}
	}
	h.agent.Trace.Step(fmt.Sprintf("step %d: prompt (%d messages)", h.step, len(ms)), b.String())
}

func (h *handler) HandleLLMGenerateContentEnd(_ context.Context, res *llms.ContentResponse) {
	if res == nil || len(res.Choices) == 0 {
		return
	}
	c := res.Choices[0]
	out := map[string]any{"content": c.Content}
	var calls []map[string]any
	for _, tc := range c.ToolCalls {
		calls = append(calls, map[string]any{"name": tc.FunctionCall.Name, "arguments": tc.FunctionCall.Arguments})
	}
	if calls != nil {
		out["tool_calls"] = calls
	}
	h.agent.Trace.Step(fmt.Sprintf("step %d: model (%v in / %v out tokens)", h.step,
		c.GenerationInfo["PromptTokens"], c.GenerationInfo["CompletionTokens"]), out)
}

func (h *handler) HandleToolEnd(_ context.Context, output string) {
	h.agent.Trace.Step(fmt.Sprintf("step %d: tool result", h.step), output)
}

func (h *handler) HandleAgentFinish(_ context.Context, _ schema.AgentFinish) { h.step = 0 }

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
