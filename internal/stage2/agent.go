// Package stage2 is a tool-calling agent with a hand-written loop.
//
// Lesson: in stage 1 our code decided what happens after the model answers.
// Here the model decides. We describe a tool (get_joke), and the model chooses
// when to call it, with what arguments, and what to do with the result,
// including retrying with different filters when JokeAPI finds nothing.
//
// The whole "agent" is the for-loop in Respond:
//
//	send messages + tool list → model replies
//	  ├─ reply has tool calls?  run them, append results, loop
//	  └─ plain text?            that's the answer, stop
//
// Everything else (frameworks, SDK tool runners) is a convenience over this.
package stage2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/ollama"
	"github.com/AriT93/agent-lab/internal/trace"
)

const systemPrompt = `You are a joke assistant. You get jokes only by calling the get_joke tool; never write a joke yourself.

Turn the user's request into get_joke filters:
- categories: only those asked for or clearly implied; [] means any.
- type: "single" for one-liners, "twopart" for setup/punchline, otherwise "any".
- blacklist: content the user wants excluded. "Clean" or "family friendly" means every flag.
  If the user says something is fine (e.g. "it can be dirty"), don't blacklist it.
- contains: a literal substring of the joke text. Multi-word phrases almost never match;
  prefer one short word, or "".

If get_joke reports no matching joke, call it again with broader filters: first shorten or
drop "contains", then loosen categories. Never loosen the blacklist. After 3 failed calls,
tell the user you couldn't find one.

When you have a joke, reply with its text exactly as returned. If you had to relax a
filter, add one short sentence saying what you changed.`

var getJokeTool = ollama.Tool{
	Type: "function",
	Function: ollama.ToolFunction{
		Name:        "get_joke",
		Description: "Fetch one random joke from JokeAPI matching the filters. Returns the joke, or an error if nothing matches.",
		Parameters:  jokeapi.Schema(),
	},
}

type Agent struct {
	LLM   *ollama.Client
	Jokes *jokeapi.Client
	Trace *trace.Tracer

	// MaxSteps caps model calls per user message. Without a cap, a confused
	// model can loop on tool calls forever, and locally each step costs seconds.
	MaxSteps int
	// Think lets the model reason before answering: better decisions, slower.
	Think bool
}

func New(llm *ollama.Client, jokes *jokeapi.Client) *Agent {
	return &Agent{LLM: llm, Jokes: jokes, MaxSteps: 6}
}

// Respond runs the agent loop for one user message and returns its reply.
func (a *Agent) Respond(ctx context.Context, text string) (string, error) {
	messages := []ollama.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: text},
	}

	for step := 1; step <= a.MaxSteps; step++ {
		resp, err := a.LLM.Chat(ctx, ollama.ChatRequest{
			Messages: messages,
			Tools:    []ollama.Tool{getJokeTool},
			Think:    &a.Think,
		})
		if err != nil {
			return "", err
		}
		msg := resp.Message
		a.Trace.Step(fmt.Sprintf("step %d: model (%d in / %d out tokens, %s)", step,
			resp.PromptEvalCount, resp.EvalCount, time.Duration(resp.TotalDuration).Round(time.Millisecond)),
			assistantSummary(msg))

		// The assistant turn, tool calls included, must go back into the
		// history so the model can see what it already asked for.
		messages = append(messages, msg)

		if len(msg.ToolCalls) == 0 {
			if resp.DoneReason == "length" {
				return msg.Content, errors.New("reply cut off by the output limit (num_predict)")
			}
			return msg.Content, nil
		}

		for _, call := range msg.ToolCalls {
			result := a.runTool(ctx, call)
			a.Trace.Step(fmt.Sprintf("step %d: %s result", step, call.Function.Name), result)
			messages = append(messages, ollama.Message{Role: "tool", ToolName: call.Function.Name, Content: result})
		}
	}
	return "", fmt.Errorf("gave up after %d steps without a final answer", a.MaxSteps)
}

// runTool executes one tool call and returns what the model will see. Errors
// are returned *to the model* as text rather than aborting the loop: telling
// the model "no matching joke" is how it learns to retry with other filters.
func (a *Agent) runTool(ctx context.Context, call ollama.ToolCall) string {
	if call.Function.Name != getJokeTool.Function.Name {
		return toJSON(map[string]string{"error": "unknown tool " + call.Function.Name})
	}

	var req jokeapi.Request
	if err := json.Unmarshal(call.Function.Arguments, &req); err != nil {
		return toJSON(map[string]string{"error": "arguments are not valid JSON for get_joke: " + err.Error()})
	}
	req = req.Normalize()
	a.Trace.Step("GET", req.URL(a.Jokes.BaseURL))

	j, err := a.Jokes.Fetch(ctx, req)
	if err != nil {
		return toJSON(map[string]string{"error": err.Error()})
	}
	return toJSON(map[string]any{"joke": j.Text(), "category": j.Category, "id": j.ID})
}

func assistantSummary(m ollama.Message) map[string]any {
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
