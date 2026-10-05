// Package ollama is a minimal client for Ollama's /api/chat endpoint.
//
// It is plain net/http + encoding/json on purpose: the request and response
// structs below *are* the wire format, so reading this file shows you exactly
// what a "tool call" or a "structured output" request looks like.
//
// The client also applies resource limits to every request, because a local
// model's defaults can be expensive: some models load with a 256K-token
// context window and the server keeps them in memory for 5 minutes.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Message is one chat turn. Assistant turns may carry ToolCalls; the results
// go back as Role "tool" messages naming the tool in ToolName.
type Message struct {
	Role      string     `json:"role"` // system, user, assistant, tool
	Content   string     `json:"content"`
	Thinking  string     `json:"thinking,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	ToolName  string     `json:"tool_name,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id,omitempty"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

// Tool describes a function the model may call. Parameters is a JSON schema.
type Tool struct {
	Type     string       `json:"type"` // always "function"
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type Options struct {
	NumCtx     int `json:"num_ctx,omitempty"`     // context window in tokens; drives KV-cache memory
	NumPredict int `json:"num_predict,omitempty"` // max tokens to generate
}

type ChatRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	Tools     []Tool    `json:"tools,omitempty"`
	Format    any       `json:"format,omitempty"` // JSON schema for structured output
	Think     *bool     `json:"think,omitempty"`
	Stream    bool      `json:"stream"`
	KeepAlive string    `json:"keep_alive,omitempty"` // how long the model stays loaded after this call
	Options   *Options  `json:"options,omitempty"`
}

type ChatResponse struct {
	Model           string  `json:"model"`
	Message         Message `json:"message"`
	DoneReason      string  `json:"done_reason"`
	PromptEvalCount int     `json:"prompt_eval_count"` // input tokens
	EvalCount       int     `json:"eval_count"`        // output tokens
	TotalDuration   int64   `json:"total_duration"`    // nanoseconds
	LoadDuration    int64   `json:"load_duration"`     // nanoseconds spent loading the model
}

const DefaultModel = "qwen3.5:9b-mlx"

// Client sends chat requests with the limits below filled in.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Model   string

	// Limits. Zero means "use Ollama's default", which is usually what eats memory.
	NumCtx     int
	NumPredict int
	KeepAlive  string
}

// New returns a client for $OLLAMA_HOST (default http://localhost:11434) with
// conservative limits: an 8K context, 2K tokens of output, unload after 2m idle.
func New() *Client {
	base := os.Getenv("OLLAMA_HOST")
	if base == "" {
		base = "http://localhost:11434"
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	return &Client{
		BaseURL:    strings.TrimRight(base, "/"),
		HTTP:       &http.Client{},
		Model:      DefaultModel,
		NumCtx:     8192,
		NumPredict: 2048,
		KeepAlive:  "2m",
	}
}

// Chat sends one non-streaming request. Model and limits come from the client
// unless the request sets them.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if req.Model == "" {
		req.Model = c.Model
	}
	if req.KeepAlive == "" {
		req.KeepAlive = c.KeepAlive
	}
	if req.Options == nil {
		req.Options = &Options{NumCtx: c.NumCtx, NumPredict: c.NumPredict}
	}

	var resp ChatResponse
	err := c.post(ctx, "/api/chat", req, &resp)
	return resp, err
}

// Unload asks the server to drop the model from memory now rather than
// waiting for KeepAlive to expire.
func (c *Client) Unload(ctx context.Context) error {
	return c.post(ctx, "/api/chat", ChatRequest{Model: c.Model, Messages: []Message{}, KeepAlive: "0s"}, nil)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ollama: %w (is `ollama serve` running?)", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("ollama: reading response: %w", err)
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return fmt.Errorf("ollama: %s", e.Error)
		}
		return fmt.Errorf("ollama: HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("ollama: decoding response: %w", err)
	}
	return nil
}
