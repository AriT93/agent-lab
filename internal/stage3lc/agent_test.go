package stage3lc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AriT93/agent-lab/internal/dadjoke"
	"github.com/AriT93/agent-lab/internal/jokeapi"
)

// scriptedModel is an OpenAI-compatible /chat/completions server that replays
// canned assistant messages and records the requests it got.
func scriptedModel(t *testing.T, replies ...map[string]any) (string, *[]map[string]any) {
	t.Helper()
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		seen = append(seen, req)
		if len(seen) > len(replies) {
			t.Fatalf("model called %d times, only %d replies scripted", len(seen), len(replies))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": replies[len(seen)-1], "finish_reason": "stop"}},
			"usage":   map[string]int{"prompt_tokens": 100, "completion_tokens": 10},
		})
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &seen
}

func toolCall(name, args string) map[string]any {
	return map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
		"id": "c1", "type": "function", "function": map[string]any{"name": name, "arguments": args},
	}}}
}

func say(text string) map[string]any { return map[string]any{"role": "assistant", "content": text} }

func fakeSources(t *testing.T) (*jokeapi.Client, *dadjoke.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/joke/"):
			w.Write([]byte(`{"error":false,"type":"single","joke":"The only JokeAPI joke.","category":"Misc","id":1}`))
		case r.URL.Path == "/search" && r.URL.Query().Get("term") == "dog":
			w.Write([]byte(`{"results":[{"id":"d1","joke":"A dog joke."}]}`))
		default:
			w.Write([]byte(`{"results":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return &jokeapi.Client{BaseURL: srv.URL + "/joke", HTTP: srv.Client()},
		&dadjoke.Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestToolCallAndMemory(t *testing.T) {
	url, seen := scriptedModel(t,
		toolCall("search_dad_jokes", `{"__arg1":"dog"}`),
		say("A dog joke."),
		say("It's a pun."),
	)
	jokes, dads := fakeSources(t)
	a, err := New(Config{BaseURL: url, Model: "fake", MaxTokens: 123}, jokes, dads)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if got, err := a.Respond(ctx, "a joke about dogs"); err != nil || got != "A dog joke." {
		t.Fatalf("turn 1 = %q, %v", got, err)
	}
	if got := (*seen)[0]["max_tokens"]; got != float64(123) {
		t.Errorf("max_tokens = %v, want the configured output cap; request: %v", got, (*seen)[0])
	}
	// The tool result went back to the model in the second request.
	second, _ := json.Marshal((*seen)[1]["messages"])
	if !strings.Contains(string(second), "A dog joke.") || !strings.Contains(string(second), "icanhazdadjoke") {
		t.Errorf("tool result missing from second call: %s", second)
	}

	if _, err := a.Respond(ctx, "explain it"); err != nil {
		t.Fatal(err)
	}
	third, _ := json.Marshal((*seen)[2]["messages"])
	if !strings.Contains(string(third), "a joke about dogs") || !strings.Contains(string(third), "A dog joke.") {
		t.Errorf("turn 1 not in memory for turn 2: %s", third)
	}

	a.Reset()
	if len(a.seen) != 0 {
		t.Error("Reset kept seen jokes")
	}
}

func TestToolErrorsAreText(t *testing.T) {
	jokes, dads := fakeSources(t)
	seen := map[string]bool{}

	out, err := jokeAPITool{jokes, seen}.Call(context.Background(), "not json")
	if err != nil || !strings.Contains(out, `"error"`) {
		t.Errorf("bad JSON: out=%q err=%v", out, err)
	}
	out, err = dadJokeTool{dads, seen}.Call(context.Background(), "unicorn")
	if err != nil || !strings.Contains(out, `"error"`) {
		t.Errorf("no match: out=%q err=%v", out, err)
	}
}

func TestRepeatedJokeIsRefused(t *testing.T) {
	jokes, _ := fakeSources(t)
	tool := jokeAPITool{jokes, map[string]bool{}}
	first, _ := tool.Call(context.Background(), `{}`)
	if !strings.Contains(first, "The only JokeAPI joke.") {
		t.Fatalf("first = %s", first)
	}
	second, _ := tool.Call(context.Background(), `{}`)
	if !strings.Contains(second, "already told") {
		t.Errorf("second = %s", second)
	}
}

// An empty tool input (a random dad joke) used to be replayed to the model as
// arguments "", which Ollama rejects with 400. It must go back as a JSON object.
func TestEmptyToolInputIsReplayedAsObject(t *testing.T) {
	url, seen := scriptedModel(t, toolCall("search_dad_jokes", `{"__arg1":""}`), say("A joke."))
	jokes, dads := fakeSources(t)
	a, err := New(Config{BaseURL: url, Model: "fake"}, jokes, dads)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Respond(context.Background(), "tell me a joke"); err != nil {
		t.Fatal(err)
	}
	second, _ := json.Marshal((*seen)[1]["messages"])
	if strings.Contains(string(second), `"arguments":""`) {
		t.Errorf("empty arguments replayed: %s", second)
	}
}

func TestOnToolSeesEveryCall(t *testing.T) {
	url, _ := scriptedModel(t, toolCall("search_dad_jokes", `{"__arg1":"dog"}`), say("A dog joke."))
	jokes, dads := fakeSources(t)
	a, err := New(Config{BaseURL: url, Model: "fake"}, jokes, dads)
	if err != nil {
		t.Fatal(err)
	}
	var names, inputs, results []string
	a.OnTool = func(name, input, result string) {
		names, inputs, results = append(names, name), append(inputs, input), append(results, result)
	}
	if _, err := a.Respond(context.Background(), "a joke about dogs"); err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "search_dad_jokes" || inputs[0] != "dog" || !strings.Contains(results[0], "A dog joke.") {
		t.Errorf("OnTool saw names=%v inputs=%v results=%v", names, inputs, results)
	}
}
