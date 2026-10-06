// Package stage6 is the stage 3 agent running on any model provider.
//
// Lesson: once the loop talks to a small interface (internal/provider)
// instead of one vendor's structs, switching between a local model, OpenAI and
// Claude is a flag. The agent below has no idea which one it got. What *does*
// change is behaviour: run the same prompts with -provider ollama / openai /
// claude through jokes-eval and compare pass rates. Differences in tool
// choice, restriction handling and verbosity are the point of this stage.
//
// Two other things differ from stage 3:
//
//   - The tools are stage 4's (the same ones the MCP server exposes), so one
//     implementation serves the agent here and MCP clients elsewhere. They only
//     return text, so the "don't repeat a joke" memory moved from inside the
//     tools to the agent loop (see run).
//   - Trimming uses a token budget that may be zero. A hosted model has a
//     context window far bigger than a 16 GB Mac can afford, so by default
//     only the turn limit applies; for Ollama the CLI passes 3/4 of num_ctx.
package stage6

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AriT93/agent-lab/internal/provider"
	"github.com/AriT93/agent-lab/internal/stage4"
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

type Agent struct {
	LLM   provider.Provider
	Trace *trace.Tracer

	MaxSteps    int // model calls per user message
	MaxTurns    int // user messages kept in history
	TokenBudget int // trim when the last prompt exceeded this many tokens; 0 = no limit

	// OnTool, if set, sees every tool call and its result (used by evals).
	OnTool func(name string, args json.RawMessage, result string)

	tools   map[string]stage4.Tool
	defs    []provider.Tool
	seen    map[string]bool // jokes already told
	history []provider.Message
	turns   []int // index in history where each user turn starts
	lastIn  int
}

// New builds an agent over llm using the given tools (e.g. stage4.NewServer(...).Tools).
func New(llm provider.Provider, tools []stage4.Tool) *Agent {
	a := &Agent{LLM: llm, MaxSteps: 6, MaxTurns: 10, tools: map[string]stage4.Tool{}, seen: map[string]bool{}}
	for _, t := range tools {
		a.tools[t.Name] = t
		a.defs = append(a.defs, provider.Tool{Name: t.Name, Description: t.Description, Parameters: t.InputSchema})
	}
	return a
}

// Reset forgets the conversation and the jokes already told.
func (a *Agent) Reset() {
	a.history, a.turns, a.lastIn = nil, nil, 0
	clear(a.seen)
}

// Respond adds the user's message to the conversation and runs the agent loop.
func (a *Agent) Respond(ctx context.Context, text string) (string, error) {
	a.trim()
	a.turns = append(a.turns, len(a.history))
	a.history = append(a.history, provider.Message{Role: "user", Content: text})

	for step := 1; step <= a.MaxSteps; step++ {
		resp, err := a.LLM.Chat(ctx, systemPrompt, a.history, a.defs)
		if err != nil {
			return "", err
		}
		a.lastIn = resp.Usage.In
		msg := resp.Message
		for i := range msg.ToolCalls { // Ollama sends no IDs; Claude and OpenAI need them to pair results
			if msg.ToolCalls[i].ID == "" {
				msg.ToolCalls[i].ID = fmt.Sprintf("call_%d_%d", len(a.history), i)
			}
		}
		a.Trace.Step(fmt.Sprintf("step %d: %s (%d in / %d out tokens, stop=%s, %d turns in memory)", step,
			a.LLM.Name(), resp.Usage.In, resp.Usage.Out, resp.Stop, len(a.turns)), summary(msg))
		a.history = append(a.history, msg)

		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}
		for _, call := range msg.ToolCalls {
			result, failed := a.run(ctx, call)
			a.Trace.Step(fmt.Sprintf("step %d: %s result", step, call.Name), result)
			if a.OnTool != nil {
				a.OnTool(call.Name, call.Arguments, result)
			}
			a.history = append(a.history, provider.Message{
				Role: "tool", ToolID: call.ID, ToolName: call.Name, Content: result, IsError: failed,
			})
		}
	}
	return "", fmt.Errorf("gave up after %d steps without a final answer", a.MaxSteps)
}

// run executes one tool call. Failures go back to the model as text so it can
// react (retry, switch tools) instead of the whole turn failing. A joke that
// was already told in this conversation counts as a failure too.
func (a *Agent) run(ctx context.Context, call provider.ToolCall) (result string, failed bool) {
	t, ok := a.tools[call.Name]
	if !ok {
		return toJSON(map[string]string{"error": "unknown tool " + call.Name}), true
	}
	text, err := t.Run(ctx, call.Arguments)
	switch {
	case err != nil:
		return toJSON(map[string]string{"error": err.Error()}), true
	case a.seen[text]:
		return toJSON(map[string]string{"error": "that joke was already told in this conversation; try different filters or the other tool"}), true
	}
	a.seen[text] = true
	return toJSON(map[string]string{"joke": text, "source": call.Name}), false
}

// trim drops whole turns from the front of the history (never half a turn, or
// a tool result would lose the call it answers) until we're under MaxTurns and
// the last prompt fit the token budget.
func (a *Agent) trim() {
	for len(a.turns) > 1 && (len(a.turns) >= a.MaxTurns || (a.TokenBudget > 0 && a.lastIn > a.TokenBudget)) {
		cut := a.turns[1]
		a.Trace.Step("memory", fmt.Sprintf("dropping oldest turn (%d messages): %q", cut, a.history[0].Content))
		a.history = a.history[cut:]
		a.turns = a.turns[1:]
		for i := range a.turns {
			a.turns[i] -= cut
		}
		a.lastIn = 0 // unknown until the next call; drop one turn at a time
	}
}

func summary(m provider.Message) map[string]any {
	out := map[string]any{}
	if m.Content != "" {
		out["content"] = m.Content
	}
	var calls []map[string]any
	for _, c := range m.ToolCalls {
		calls = append(calls, map[string]any{"id": c.ID, "name": c.Name, "arguments": c.Arguments})
	}
	if calls != nil {
		out["tool_calls"] = calls
	}
	return out
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
