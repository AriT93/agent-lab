package stage2

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/ollama"
)

// scriptedModel replays canned assistant messages in order and records every
// request, so a test can check what the loop sent back to the model.
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
		json.NewEncoder(w).Encode(ollama.ChatResponse{Message: replies[len(seen)-1], DoneReason: "stop"})
	}))
	t.Cleanup(srv.Close)
	c := ollama.New()
	c.BaseURL = srv.URL
	return c, &seen
}

func toolCall(args string) ollama.Message {
	var c ollama.ToolCall
	c.Function.Name = "get_joke"
	c.Function.Arguments = json.RawMessage(args)
	return ollama.Message{Role: "assistant", ToolCalls: []ollama.ToolCall{c}}
}

// fakeJokes answers "no match" whenever a contains filter is set.
func fakeJokes(t *testing.T) *jokeapi.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("contains") != "" {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":true,"code":106,"message":"No matching joke found"}`))
			return
		}
		w.Write([]byte(`{"error":false,"type":"single","joke":"A baseball joke.","category":"Misc","id":7}`))
	}))
	t.Cleanup(srv.Close)
	return &jokeapi.Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestRetriesAfterNoMatch(t *testing.T) {
	llm, seen := scriptedModel(t,
		toolCall(`{"categories":[],"type":"any","blacklist":["racist"],"contains":"chicago cubs"}`),
		toolCall(`{"categories":["Misc"],"type":"any","blacklist":["racist"],"contains":""}`),
		ollama.Message{Role: "assistant", Content: "A baseball joke.\n(Nothing mentioned the Cubs, so here's a general one.)"},
	)
	agent := New(llm, fakeJokes(t))

	got, err := agent.Respond(context.Background(), "a cubs joke, nothing racist")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "A baseball joke.") {
		t.Errorf("reply = %q", got)
	}

	// The second model call must have seen the failed tool result...
	second := (*seen)[1].Messages
	last := second[len(second)-1]
	if last.Role != "tool" || !strings.Contains(last.Content, "No matching joke") {
		t.Errorf("model did not get the error back: %+v", last)
	}
	// ...and every call must offer the tool and apply the client's limits.
	for i, req := range *seen {
		if len(req.Tools) != 1 || req.Options == nil || req.Options.NumCtx == 0 || req.KeepAlive == "" {
			t.Errorf("call %d missing tools or limits: %+v", i, req)
		}
	}
}

func TestStopsAtMaxSteps(t *testing.T) {
	loop := toolCall(`{"categories":[],"type":"any","blacklist":[],"contains":"x"}`)
	llm, _ := scriptedModel(t, loop, loop, loop)
	agent := New(llm, fakeJokes(t))
	agent.MaxSteps = 3

	if _, err := agent.Respond(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "gave up") {
		t.Fatalf("want gave-up error, got %v", err)
	}
}
