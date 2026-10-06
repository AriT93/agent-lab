package stage6

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AriT93/agent-lab/internal/provider"
	"github.com/AriT93/agent-lab/internal/stage4"
)

// fakeProvider replays scripted replies and records what it was sent.
type fakeProvider struct {
	t       *testing.T
	replies []provider.Message
	sent    [][]provider.Message
}

func (f *fakeProvider) Name() string { return "fake/model" }

func (f *fakeProvider) Chat(_ context.Context, _ string, msgs []provider.Message, _ []provider.Tool) (provider.Response, error) {
	f.sent = append(f.sent, append([]provider.Message(nil), msgs...))
	if len(f.sent) > len(f.replies) {
		f.t.Fatalf("model called %d times, only %d replies scripted", len(f.sent), len(f.replies))
	}
	return provider.Response{Message: f.replies[len(f.sent)-1], Usage: provider.Usage{In: 100, Out: 10}, Stop: "stop"}, nil
}

func call(name, args string) provider.Message {
	return provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{{Name: name, Arguments: json.RawMessage(args)}}}
}

func say(s string) provider.Message { return provider.Message{Role: "assistant", Content: s} }

// jokeTools returns tools that always yield the same text, to test repeats.
func jokeTools() []stage4.Tool {
	return []stage4.Tool{{
		Name: "get_joke", Description: "d", InputSchema: map[string]any{"type": "object"},
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			if strings.Contains(string(args), "fail") {
				return "", context.DeadlineExceeded
			}
			return "The joke.", nil
		},
	}}
}

func TestToolLoopAndMemory(t *testing.T) {
	f := &fakeProvider{t: t, replies: []provider.Message{call("get_joke", `{}`), say("The joke."), say("Because.")}}
	a := New(f, jokeTools())
	var seen []string
	a.OnTool = func(name string, _ json.RawMessage, result string) { seen = append(seen, name+" "+result) }
	ctx := context.Background()

	if got, err := a.Respond(ctx, "joke"); err != nil || got != "The joke." {
		t.Fatalf("turn 1 = %q, %v", got, err)
	}
	if len(seen) != 1 || seen[0] != `get_joke {"joke":"The joke.","source":"get_joke"}` {
		t.Errorf("OnTool saw %v", seen)
	}
	// The second model call carries the tool result, paired to the call by an ID we filled in.
	second := f.sent[1]
	if len(second) != 3 || second[2].Role != "tool" || second[2].ToolID == "" || second[2].ToolID != second[1].ToolCalls[0].ID {
		t.Fatalf("second call history = %+v", second)
	}

	if _, err := a.Respond(ctx, "explain it"); err != nil {
		t.Fatal(err)
	}
	if n := len(f.sent[2]); n != 5 { // turn 1 (4 messages) + the new user message
		t.Errorf("third call had %d messages, want the whole first turn kept", n)
	}
}

func TestRepeatsAndFailuresAreToolErrors(t *testing.T) {
	f := &fakeProvider{t: t, replies: []provider.Message{
		call("get_joke", `{}`), call("get_joke", `{}`), call("get_joke", `{"fail":1}`), call("nope", `{}`), say("sorry"),
	}}
	a := New(f, jokeTools())
	a.MaxSteps = 5
	if _, err := a.Respond(context.Background(), "joke"); err != nil {
		t.Fatal(err)
	}
	h := a.history
	var tools []provider.Message
	for _, m := range h {
		if m.Role == "tool" {
			tools = append(tools, m)
		}
	}
	if len(tools) != 4 || tools[0].IsError || !tools[1].IsError || !tools[2].IsError || !tools[3].IsError {
		t.Fatalf("tool results = %+v", tools)
	}
	if !strings.Contains(tools[1].Content, "already told") || !strings.Contains(tools[3].Content, "unknown tool") {
		t.Errorf("results = %q / %q", tools[1].Content, tools[3].Content)
	}
}

func TestTrimByTurnsAndTokens(t *testing.T) {
	replies := []provider.Message{say("a"), say("b"), say("c"), say("d")}
	f := &fakeProvider{t: t, replies: replies}
	a := New(f, jokeTools())
	a.MaxTurns = 2
	for _, q := range []string{"1", "2", "3"} {
		if _, err := a.Respond(context.Background(), q); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.turns) != 2 || a.history[0].Content != "2" {
		t.Errorf("after 3 turns with MaxTurns=2: turns=%v first=%q", a.turns, a.history[0].Content)
	}

	// Token budget: the fake reports 100 prompt tokens, over a budget of 50.
	a = New(&fakeProvider{t: t, replies: replies}, jokeTools())
	a.TokenBudget = 50
	a.Respond(context.Background(), "1")
	a.Respond(context.Background(), "2")
	a.Respond(context.Background(), "3")
	if len(a.turns) != 2 {
		t.Errorf("token budget: turns=%v, want the oldest dropped each time", a.turns)
	}
}

func TestGivesUpAfterMaxSteps(t *testing.T) {
	f := &fakeProvider{t: t, replies: []provider.Message{call("get_joke", `{"fail":1}`), call("get_joke", `{"fail":1}`)}}
	a := New(f, jokeTools())
	a.MaxSteps = 2
	if _, err := a.Respond(context.Background(), "joke"); err == nil || !strings.Contains(err.Error(), "gave up") {
		t.Errorf("err = %v", err)
	}
}
