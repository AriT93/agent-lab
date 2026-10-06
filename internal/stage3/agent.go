// Package stage3 is a multi-tool agent that remembers the conversation.
//
// New compared with stage 2:
//
//   - Two tools (JokeAPI and icanhazdadjoke). The model has to pick one from
//     the descriptions alone, which makes tool descriptions part of the prompt.
//   - Memory. The message history persists across user messages, so "another
//     one", "explain it" and "keep it clean from now on" work. The model has no
//     memory of its own: "memory" is just us re-sending the transcript.
//   - Context management. The transcript grows every turn but the context window
//     (num_ctx) doesn't. When it gets close, we drop the oldest turns. Notice
//     the trade-off: a restriction the user gave early on ("nothing racist")
//     can be trimmed away. Real systems summarize or pin such facts instead.
package stage3

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AriT93/agent-lab/internal/dadjoke"
	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/ollama"
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
	LLM   *ollama.Client
	Trace *trace.Tracer

	MaxSteps int  // model calls per user message
	MaxTurns int  // user messages kept in history
	Think    bool // let the model reason before acting

	// OnTool, if set, sees every tool call and its result (used by evals).
	OnTool func(name string, args json.RawMessage, result string)

	tools   map[string]Tool
	defs    []ollama.Tool
	seen    seenJokes
	history []ollama.Message // without the system prompt
	turns   []int            // index in history where each user turn starts
	lastIn  int              // prompt tokens of the most recent model call
}

func New(llm *ollama.Client, jokes *jokeapi.Client, dads *dadjoke.Client) *Agent {
	a := &Agent{LLM: llm, MaxSteps: 6, MaxTurns: 10, tools: map[string]Tool{}, seen: seenJokes{}}
	for _, t := range []Tool{jokeAPITool(jokes, a.seen), dadJokeTool(dads, a.seen)} {
		a.tools[t.Def.Function.Name] = t
		a.defs = append(a.defs, t.Def)
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
	a.history = append(a.history, ollama.Message{Role: "user", Content: text})

	for step := 1; step <= a.MaxSteps; step++ {
		messages := append([]ollama.Message{{Role: "system", Content: systemPrompt}}, a.history...)
		resp, err := a.LLM.Chat(ctx, ollama.ChatRequest{Messages: messages, Tools: a.defs, Think: &a.Think})
		if err != nil {
			return "", err
		}
		a.lastIn = resp.PromptEvalCount
		msg := resp.Message
		a.Trace.Step(fmt.Sprintf("step %d: model (%d in / %d out tokens, %s, %d turns in memory)", step,
			resp.PromptEvalCount, resp.EvalCount, time.Duration(resp.TotalDuration).Round(time.Millisecond), len(a.turns)),
			summary(msg))
		a.history = append(a.history, msg)

		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}
		for _, call := range msg.ToolCalls {
			result := a.run(ctx, call)
			a.Trace.Step(fmt.Sprintf("step %d: %s result", step, call.Function.Name), result)
			if a.OnTool != nil {
				a.OnTool(call.Function.Name, call.Function.Arguments, result)
			}
			a.history = append(a.history, ollama.Message{Role: "tool", ToolName: call.Function.Name, Content: result})
		}
	}
	return "", fmt.Errorf("gave up after %d steps without a final answer", a.MaxSteps)
}

// run executes one tool call. Failures go back to the model as text so it can
// react (retry, switch tools) instead of the whole turn failing.
func (a *Agent) run(ctx context.Context, call ollama.ToolCall) string {
	t, ok := a.tools[call.Function.Name]
	if !ok {
		return toJSON(map[string]string{"error": "unknown tool " + call.Function.Name})
	}
	out, err := t.Run(ctx, call.Function.Arguments)
	if err != nil {
		return toJSON(map[string]string{"error": err.Error()})
	}
	return toJSON(out)
}

// trim drops whole turns from the front of the history (never half a turn, or
// a tool result would lose the call it answers) until we're under MaxTurns and
// the last prompt used less than 3/4 of the context window.
func (a *Agent) trim() {
	budget := a.LLM.NumCtx * 3 / 4
	for len(a.turns) > 1 && (len(a.turns) >= a.MaxTurns || (budget > 0 && a.lastIn > budget)) {
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

func summary(m ollama.Message) map[string]any {
	out := map[string]any{}
	if m.Thinking != "" {
		out["thinking"] = m.Thinking
	}
	if m.Content != "" {
		out["content"] = m.Content
	}
	var calls []map[string]any
	for _, c := range m.ToolCalls {
		calls = append(calls, map[string]any{"name": c.Function.Name, "arguments": c.Function.Arguments})
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
