package stage4

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AriT93/agent-lab/internal/dadjoke"
	"github.com/AriT93/agent-lab/internal/jokeapi"
)

// session feeds newline-delimited requests to a server and returns the replies.
func session(t *testing.T, lines ...string) []map[string]any {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/joke/"):
			if r.URL.Query().Get("contains") == "zzz" {
				w.Write([]byte(`{"error":true,"internalError":false,"code":106,"message":"No matching joke found","causedBy":["x"]}`))
				return
			}
			w.Write([]byte(`{"error":false,"type":"single","joke":"A JokeAPI joke.","category":"Misc","id":1}`))
		default:
			w.Write([]byte(`{"results":[{"id":"d1","joke":"A dog joke."}]}`))
		}
	}))
	t.Cleanup(api.Close)
	s := NewServer(&jokeapi.Client{BaseURL: api.URL + "/joke", HTTP: api.Client()},
		&dadjoke.Client{BaseURL: api.URL, HTTP: api.Client()})

	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var replies []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("reply is not one JSON object per line: %q", l)
		}
		replies = append(replies, m)
	}
	return replies
}

func TestHandshakeAndList(t *testing.T) {
	replies := session(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`, // no reply
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(replies) != 2 {
		t.Fatalf("got %d replies, want 2 (notifications get none): %v", len(replies), replies)
	}
	init := replies[0]["result"].(map[string]any)
	if init["protocolVersion"] != ProtocolVersion {
		t.Errorf("initialize = %v", init)
	}
	tools := replies[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %v", tools)
	}
	first := tools[0].(map[string]any)
	if first["name"] != "get_joke" || first["inputSchema"] == nil || first["Run"] != nil {
		t.Errorf("tool listing = %v", first)
	}
}

func TestCallTool(t *testing.T) {
	replies := session(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_joke","arguments":{"categories":["Misc"]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search_dad_jokes","arguments":{"term":"dog"}}}`,
	)
	text := func(r map[string]any) (string, bool) {
		res := r["result"].(map[string]any)
		return res["content"].([]any)[0].(map[string]any)["text"].(string), res["isError"].(bool)
	}
	if got, isErr := text(replies[0]); got != "A JokeAPI joke." || isErr {
		t.Errorf("get_joke = %q err=%v", got, isErr)
	}
	if got, isErr := text(replies[1]); got != "A dog joke." || isErr {
		t.Errorf("search_dad_jokes = %q err=%v", got, isErr)
	}
}

func TestToolFailureIsAResultNotAnRPCError(t *testing.T) {
	replies := session(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_joke","arguments":{"contains":"zzz"}}}`)
	if replies[0]["error"] != nil {
		t.Fatalf("got a JSON-RPC error: %v", replies[0]["error"])
	}
	res := replies[0]["result"].(map[string]any)
	if res["isError"] != true || !strings.Contains(res["content"].([]any)[0].(map[string]any)["text"].(string), "No matching joke") {
		t.Errorf("result = %v", res)
	}
}

func TestProtocolErrors(t *testing.T) {
	replies := session(t,
		`not json`,
		`{"jsonrpc":"2.0","id":7,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"nope"}}`,
		`{"jsonrpc":"2.0","id":9,"method":"ping"}`,
	)
	codes := []float64{codeParse, codeMethodNotFound, codeInvalidParams}
	for i, want := range codes {
		got := replies[i]["error"].(map[string]any)["code"].(float64)
		if got != want {
			t.Errorf("reply %d code = %v, want %v", i, got, want)
		}
	}
	if replies[3]["error"] != nil || string(mustJSON(replies[3]["id"])) != "9" {
		t.Errorf("ping = %v", replies[3])
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
