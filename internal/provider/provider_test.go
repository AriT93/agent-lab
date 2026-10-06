package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AriT93/agent-lab/internal/ollama"
)

// A conversation with every message kind: user, an assistant turn with two
// parallel tool calls, their two results.
var (
	tools = []Tool{{Name: "get_joke", Description: "d", Parameters: map[string]any{"type": "object"}}}
	convo = []Message{
		{Role: "user", Content: "a joke"},
		{Role: "assistant", ToolCalls: []ToolCall{
			{ID: "c1", Name: "get_joke", Arguments: json.RawMessage(`{"contains":"a"}`)},
			{ID: "c2", Name: "get_joke", Arguments: json.RawMessage(`{"contains":"b"}`)},
		}},
		{Role: "tool", ToolID: "c1", ToolName: "get_joke", Content: "joke A"},
		{Role: "tool", ToolID: "c2", ToolName: "get_joke", Content: "no match", IsError: true},
	}
)

// serve replies with reply and decodes the request into got.
func serve(t *testing.T, status int, reply string, got *map[string]any, hdr *http.Header) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got != nil {
			json.NewDecoder(r.Body).Decode(got)
		}
		if hdr != nil {
			*hdr = r.Header.Clone()
		}
		w.WriteHeader(status)
		w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOpenAI(t *testing.T) {
	var got map[string]any
	var hdr http.Header
	srv := serve(t, 200, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[
		{"id":"x1","type":"function","function":{"name":"get_joke","arguments":"{\"type\":\"single\"}"}}]},
		"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":11,"completion_tokens":3}}`, &got, &hdr)
	p := &OpenAI{BaseURL: srv.URL, APIKey: "k", Model: "m", HTTP: srv.Client()}

	resp, err := p.Chat(context.Background(), "be funny", convo, tools)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Get("Authorization") != "Bearer k" {
		t.Errorf("auth header = %q", hdr.Get("Authorization"))
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 5 || msgs[0].(map[string]any)["role"] != "system" {
		t.Fatalf("messages = %v", msgs)
	}
	call := msgs[2].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if call["arguments"] != `{"contains":"a"}` { // a string, not an object
		t.Errorf("arguments on the wire = %#v", call["arguments"])
	}
	if msgs[3].(map[string]any)["tool_call_id"] != "c1" {
		t.Errorf("tool message = %v", msgs[3])
	}
	if len(resp.Message.ToolCalls) != 1 || string(resp.Message.ToolCalls[0].Arguments) != `{"type":"single"}` ||
		resp.Message.ToolCalls[0].ID != "x1" || resp.Usage != (Usage{11, 3}) || resp.Stop != "tool_calls" {
		t.Errorf("response = %+v", resp)
	}
}

func TestClaude(t *testing.T) {
	var got map[string]any
	var hdr http.Header
	srv := serve(t, 200, `{"content":[{"type":"text","text":"Here: "},{"type":"text","text":"joke A"}],
		"stop_reason":"end_turn","usage":{"input_tokens":20,"output_tokens":4}}`, &got, &hdr)
	p := &Claude{BaseURL: srv.URL, APIKey: "k", Model: "m", HTTP: srv.Client()}

	resp, err := p.Chat(context.Background(), "be funny", convo, tools)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Get("x-api-key") != "k" || hdr.Get("anthropic-version") == "" {
		t.Errorf("headers = %v", hdr)
	}
	if got["system"] != "be funny" || got["max_tokens"] == nil {
		t.Errorf("system/max_tokens = %v / %v", got["system"], got["max_tokens"])
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 3 { // user, assistant, ONE user message holding both results
		t.Fatalf("messages = %d, want 3: %v", len(msgs), msgs)
	}
	results := msgs[2].(map[string]any)
	blocks := results["content"].([]any)
	if results["role"] != "user" || len(blocks) != 2 {
		t.Fatalf("tool results message = %v", results)
	}
	if b := blocks[1].(map[string]any); b["type"] != "tool_result" || b["tool_use_id"] != "c2" || b["is_error"] != true {
		t.Errorf("second result = %v", b)
	}
	use := msgs[1].(map[string]any)["content"].([]any)[0].(map[string]any)
	if use["type"] != "tool_use" || use["input"].(map[string]any)["contains"] != "a" { // an object here
		t.Errorf("tool_use = %v", use)
	}
	if resp.Message.Content != "Here: joke A" || resp.Usage != (Usage{20, 4}) || resp.Stop != "end_turn" {
		t.Errorf("response = %+v", resp)
	}
}

func TestClaudeToolUseResponse(t *testing.T) {
	srv := serve(t, 200, `{"content":[{"type":"tool_use","id":"tu1","name":"get_joke","input":{"type":"single"}}],
		"stop_reason":"tool_use","usage":{}}`, nil, nil)
	p := &Claude{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	resp, err := p.Chat(context.Background(), "s", convo[:1], tools)
	if err != nil {
		t.Fatal(err)
	}
	if c := resp.Message.ToolCalls; len(c) != 1 || c[0].ID != "tu1" || string(c[0].Arguments) != `{"type":"single"}` {
		t.Errorf("tool calls = %+v", c)
	}
}

func TestOllama(t *testing.T) {
	var got map[string]any
	srv := serve(t, 200, `{"message":{"role":"assistant","content":"","tool_calls":[
		{"function":{"name":"get_joke","arguments":{"type":"single"}}}]},
		"done_reason":"stop","prompt_eval_count":7,"eval_count":2}`, &got, nil)
	c := ollama.New()
	c.BaseURL, c.HTTP = srv.URL, srv.Client()
	p := &Ollama{Client: c}

	resp, err := p.Chat(context.Background(), "be funny", convo, tools)
	if err != nil {
		t.Fatal(err)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 5 || msgs[4].(map[string]any)["tool_name"] != "get_joke" {
		t.Errorf("messages = %v", msgs)
	}
	if got["options"].(map[string]any)["num_ctx"] == nil {
		t.Error("memory limits (num_ctx) were not sent")
	}
	if len(resp.Message.ToolCalls) != 1 || string(resp.Message.ToolCalls[0].Arguments) != `{"type":"single"}` ||
		resp.Usage != (Usage{7, 2}) {
		t.Errorf("response = %+v", resp)
	}
}

func TestErrorsCarryTheServersMessage(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"openai", `{"error":{"message":"bad key","type":"auth"}}`, "bad key"},
		{"claude", `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`, "invalid x-api-key"},
		{"ollama-style", `{"error":"model not found"}`, "model not found"},
		{"opaque", `oops`, "HTTP 401"},
	} {
		srv := serve(t, 401, tc.body, nil, nil)
		_, err := (&OpenAI{BaseURL: srv.URL, HTTP: srv.Client()}).Chat(context.Background(), "s", convo[:1], nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", tc.name, err, tc.want)
		}
	}
}

func TestByName(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	if _, err := ByName("claude", "", ollama.New(), false); err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("claude without key: %v", err)
	}
	if _, err := ByName("openai", "", ollama.New(), false); err == nil {
		t.Error("openai without key should fail")
	}
	t.Setenv("OPENAI_BASE_URL", "http://localhost:11434/v1") // local server, no key needed
	if p, err := ByName("openai", "qwen", ollama.New(), false); err != nil || p.Name() != "openai/qwen" {
		t.Errorf("openai local: %v %v", p, err)
	}
	if p, err := ByName("ollama", "m", ollama.New(), false); err != nil || p.Name() != "ollama/m" {
		t.Errorf("ollama: %v %v", p, err)
	}
	if _, err := ByName("gemini", "", ollama.New(), false); err == nil {
		t.Error("unknown provider should fail")
	}
}
