package stage1

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/AriT93/agent-lab/internal/jokeapi"
)

// fakeClaude stands in for the Messages API: it records the request body and
// replies with a canned message, so these tests need no API key or network.
func fakeClaude(t *testing.T, stopReason, text string) (*Claude, *map[string]any) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		reply := map[string]any{
			"id": "msg_test", "type": "message", "role": "assistant", "model": DefaultClaudeModel,
			"content": []map[string]any{
				{"type": "thinking", "thinking": "", "signature": "sig"},
				{"type": "text", "text": text},
			},
			"stop_reason": stopReason,
			"usage":       map[string]any{"input_tokens": 10, "output_tokens": 5},
		}
		if stopReason == "refusal" {
			reply["stop_details"] = map[string]any{"type": "refusal", "category": "cyber"}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(srv.Close)
	return NewClaude(option.WithBaseURL(srv.URL), option.WithAPIKey("test"), option.WithMaxRetries(0)), &got
}

func TestInterpret(t *testing.T) {
	in, sent := fakeClaude(t, "end_turn",
		`{"categories":["Misc"],"type":"single","blacklist":["racist"],"contains":"cubs"}`)

	got, err := in.Interpret(context.Background(), "a cubs joke, can be dirty but not racist")
	if err != nil {
		t.Fatal(err)
	}
	want := jokeapi.Request{
		Categories: []jokeapi.Category{jokeapi.Misc},
		Type:       "single",
		Blacklist:  []jokeapi.Flag{jokeapi.Racist},
		Contains:   "cubs",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	// The blacklist survives all the way to the URL, which was the original bug.
	if u := got.URL(jokeapi.DefaultBaseURL); !strings.Contains(u, "blacklistFlags=racist") {
		t.Errorf("blacklist missing from URL %s", u)
	}

	// The request asked for schema-constrained output.
	format := (*sent)["output_config"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" || format["schema"] == nil {
		t.Errorf("output_config.format = %v", format)
	}
}

func TestInterpretRefusal(t *testing.T) {
	in, _ := fakeClaude(t, "refusal", "")
	_, err := in.Interpret(context.Background(), "anything")
	if err == nil || !strings.Contains(err.Error(), "declined") {
		t.Fatalf("want refusal error, got %v", err)
	}
}
