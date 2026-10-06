// Package provider puts three chat APIs (Ollama, OpenAI, Claude) behind one
// small interface, so an agent can switch models without changing its loop.
//
// Lesson: the providers all do "messages + tools in, message out", but the
// wire formats differ in ways an agent loop shouldn't care about:
//
//   - System prompt: a message (Ollama, OpenAI) vs. a top-level field (Claude).
//   - Tool call arguments: a JSON object (Ollama, Claude) vs. a JSON *string*
//     inside JSON (OpenAI).
//   - Tool results: a "tool" role message (Ollama, OpenAI) vs. a tool_result
//     block inside a *user* message (Claude), which also wants all results of
//     one assistant turn together in a single message.
//   - Tool call IDs: required to pair calls with results by OpenAI and Claude;
//     Ollama doesn't send any.
//
// Each adapter is plain net/http + encoding/json with structs that mirror its
// wire format, same as internal/ollama. The neutral types below are the
// smallest set the agent in stage 6 needs.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Message is one turn. Role is "user", "assistant" or "tool".
type Message struct {
	Role      string
	Content   string
	ToolCalls []ToolCall // assistant turns
	ToolID    string     // tool turns: the call this answers
	ToolName  string     // tool turns
	IsError   bool       // tool turns: the tool failed (Claude marks these explicitly)
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage // always a JSON object
}

// Tool describes a function the model may call. Parameters is a JSON schema.
type Tool struct {
	Name        string
	Description string
	Parameters  any
}

type Usage struct{ In, Out int }

type Response struct {
	Message Message
	Usage   Usage
	Stop    string // the provider's own stop reason, for tracing
}

// Provider sends one non-streaming chat request.
type Provider interface {
	// Name is "provider/model", for labels and traces.
	Name() string
	Chat(ctx context.Context, system string, messages []Message, tools []Tool) (Response, error)
}

// maxOutputTokens matches the Ollama client's default cap. Claude requires a
// value; the others get it so all three behave alike.
const maxOutputTokens = 2048

// postJSON sends body as JSON and decodes a 2xx reply into out. On other
// statuses it returns the server's error text (all three APIs reply
// {"error": ...} with either a string or an object with a message).
func postJSON(ctx context.Context, hc *http.Client, who, url string, headers map[string]string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", who, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s: reading response: %w", who, err)
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && len(e.Error) > 0 {
			var msg struct {
				Message string `json:"message"`
			}
			var s string
			switch {
			case json.Unmarshal(e.Error, &s) == nil:
				return fmt.Errorf("%s: %s", who, s)
			case json.Unmarshal(e.Error, &msg) == nil && msg.Message != "":
				return fmt.Errorf("%s: %s", who, msg.Message)
			}
		}
		return fmt.Errorf("%s: HTTP %d", who, resp.StatusCode)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: decoding response: %w", who, err)
	}
	return nil
}

// objectOrEmpty makes sure tool arguments are a JSON object.
func objectOrEmpty(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
		return json.RawMessage("{}")
	}
	return raw
}
