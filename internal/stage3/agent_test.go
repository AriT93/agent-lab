package stage3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AriT93/agent-lab/internal/dadjoke"
	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/ollama"
)

func scriptedModel(t *testing.T, replies ...ollama.Message) (*ollama.Client, *[]ollama.ChatRequest) {
	t.Helper()
	var seen []ollama.ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollama.ChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		seen = append(seen, req)
		if len(seen) > len(replies) {
			t.Fatalf("model called %d times, only %d replies scripted", len(seen), len(replies))
		}
		json.NewEncoder(w).Encode(ollama.ChatResponse{Message: replies[len(seen)-1], DoneReason: "stop", PromptEvalCount: 100})
	}))
	t.Cleanup(srv.Close)
	c := ollama.New()
	c.BaseURL = srv.URL
	return c, &seen
}

func call(name, args string) ollama.Message {
	var c ollama.ToolCall
	c.Function.Name = name
	c.Function.Arguments = json.RawMessage(args)
	return ollama.Message{Role: "assistant", ToolCalls: []ollama.ToolCall{c}}
}

func say(text string) ollama.Message { return ollama.Message{Role: "assistant", Content: text} }

// fakeSources serves JokeAPI (always joke id 1) and icanhazdadjoke (one dog joke).
func fakeSources(t *testing.T) (*jokeapi.Client, *dadjoke.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/joke/"):
			w.Write([]byte(`{"error":false,"type":"single","joke":"The only JokeAPI joke.","category":"Misc","id":1}`))
		case r.URL.Path == "/search" && r.URL.Query().Get("term") == "dog":
			w.Write([]byte(`{"results":[{"id":"d1","joke":"A dog joke."}]}`))
		case r.URL.Path == "/search":
			w.Write([]byte(`{"results":[]}`))
		default:
			w.Write([]byte(`{"id":"r1","joke":"A random dad joke."}`))
		}
	}))
	t.Cleanup(srv.Close)
	return &jokeapi.Client{BaseURL: srv.URL + "/joke", HTTP: srv.Client()},
		&dadjoke.Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestMemoryAcrossTurns(t *testing.T) {
	llm, seen := scriptedModel(t,
		call("search_dad_jokes", `{"term":"dog"}`),
		say("A dog joke."),
		say("It's a pun on ..."), // "explain it": answered from history, no tool
	)
	jokes, dads := fakeSources(t)
	a := New(llm, jokes, dads)
	ctx := context.Background()

	if got, err := a.Respond(ctx, "a joke about dogs"); err != nil || got != "A dog joke." {
		t.Fatalf("turn 1 = %q, %v", got, err)
	}
	if _, err := a.Respond(ctx, "explain it"); err != nil {
		t.Fatal(err)
	}

	// The third model call must carry the whole first turn: user, tool call,
	// tool result, answer, then the new question. That *is* the memory.
	msgs := (*seen)[2].Messages
	var roles []string
	for _, m := range msgs {
		roles = append(roles, m.Role)
	}
	want := "system user assistant tool assistant user"
	if got := strings.Join(roles, " "); got != want {
		t.Errorf("roles = %q, want %q", got, want)
	}
	if !strings.Contains(msgs[3].Content, "A dog joke.") {
		t.Errorf("tool result missing from history: %+v", msgs[3])
	}
}

func TestTrimDropsWholeTurns(t *testing.T) {
	llm, seen := scriptedModel(t,
		call("search_dad_jokes", `{"term":"dog"}`), say("one"),
		say("two"),
		say("three"),
	)
	jokes, dads := fakeSources(t)
	a := New(llm, jokes, dads)
	a.MaxTurns = 2
	ctx := context.Background()

	for _, q := range []string{"first", "second", "third"} {
		if _, err := a.Respond(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	last := (*seen)[len(*seen)-1].Messages
	if last[1].Role != "user" || last[1].Content != "second" {
		t.Errorf("history should start at turn 2, got %+v", last[1])
	}
	for _, m := range last {
		if m.Role == "tool" {
			t.Errorf("tool result from the dropped turn is still in history")
		}
	}
}

func TestRepeatsAreFiltered(t *testing.T) {
	llm, seen := scriptedModel(t,
		call("get_joke", `{"categories":[],"type":"any","blacklist":[],"contains":""}`),
		say("The only JokeAPI joke."),
		call("get_joke", `{"categories":[],"type":"any","blacklist":[],"contains":""}`),
		say("Sorry, I'm out of those."),
	)
	jokes, dads := fakeSources(t)
	a := New(llm, jokes, dads)
	ctx := context.Background()

	a.Respond(ctx, "a joke")
	a.Respond(ctx, "another one")

	msgs := (*seen)[3].Messages
	if res := msgs[len(msgs)-1]; res.Role != "tool" || !strings.Contains(res.Content, "already told") {
		t.Errorf("want repeat error in tool result, got %+v", res)
	}

	a.Reset()
	if len(a.history) != 0 || len(a.seen) != 0 {
		t.Error("Reset left state behind")
	}
}

func TestUnknownToolIsReportedToModel(t *testing.T) {
	llm, seen := scriptedModel(t, call("write_joke", `{}`), say("ok"))
	jokes, dads := fakeSources(t)
	if _, err := New(llm, jokes, dads).Respond(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	msgs := (*seen)[1].Messages
	if !strings.Contains(msgs[len(msgs)-1].Content, "unknown tool") {
		t.Errorf("model was not told about the unknown tool")
	}
}
