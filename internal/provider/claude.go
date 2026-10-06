package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

var errNoChoices = errors.New("provider: response had no choices")

const DefaultClaudeModel = "claude-sonnet-5-5"

// Claude speaks the Messages API over plain HTTP. (Stage 1's Claude backend
// uses the SDK; this one doesn't, so the wire format is visible.)
type Claude struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

// NewClaude reads ANTHROPIC_API_KEY (a Console key; a Claude Pro/Max
// subscription doesn't include API access) and ANTHROPIC_BASE_URL.
func NewClaude() *Claude {
	base := os.Getenv("ANTHROPIC_BASE_URL")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	return &Claude{
		BaseURL: strings.TrimRight(base, "/"), APIKey: os.Getenv("ANTHROPIC_API_KEY"),
		Model: DefaultClaudeModel, HTTP: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *Claude) Name() string { return "claude/" + c.Model }

// Wire format. A message's content is a list of typed blocks.
type claudeBlock struct {
	Type      string          `json:"type"` // text, tool_use, tool_result
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`    // tool_use
	Name      string          `json:"name,omitempty"`  // tool_use
	Input     json.RawMessage `json:"input,omitempty"` // tool_use
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"` // tool_result
	IsError   bool            `json:"is_error,omitempty"`
}

type claudeMessage struct {
	Role    string        `json:"role"` // user or assistant
	Content []claudeBlock `json:"content"`
}

type claudeTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type claudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system"`
	Messages  []claudeMessage `json:"messages"`
	Tools     []claudeTool    `json:"tools,omitempty"`
}

type claudeResponse struct {
	Content    []claudeBlock `json:"content"`
	StopReason string        `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (c *Claude) Chat(ctx context.Context, system string, messages []Message, tools []Tool) (Response, error) {
	req := claudeRequest{Model: c.Model, MaxTokens: maxOutputTokens, System: system}
	for _, m := range messages {
		switch m.Role {
		case "tool":
			block := claudeBlock{Type: "tool_result", ToolUseID: m.ToolID, Content: m.Content, IsError: m.IsError}
			// Results of one assistant turn share one user message.
			if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "user" &&
				req.Messages[n-1].Content[0].Type == "tool_result" {
				req.Messages[n-1].Content = append(req.Messages[n-1].Content, block)
			} else {
				req.Messages = append(req.Messages, claudeMessage{Role: "user", Content: []claudeBlock{block}})
			}
		case "assistant":
			cm := claudeMessage{Role: "assistant"}
			if m.Content != "" {
				cm.Content = append(cm.Content, claudeBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				cm.Content = append(cm.Content, claudeBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: objectOrEmpty(tc.Arguments)})
			}
			req.Messages = append(req.Messages, cm)
		default:
			req.Messages = append(req.Messages, claudeMessage{Role: "user", Content: []claudeBlock{{Type: "text", Text: m.Content}}})
		}
	}
	for _, t := range tools {
		req.Tools = append(req.Tools, claudeTool{Name: t.Name, Description: t.Description, InputSchema: t.Parameters})
	}

	var resp claudeResponse
	err := postJSON(ctx, c.HTTP, "claude", c.BaseURL+"/v1/messages",
		map[string]string{"x-api-key": c.APIKey, "anthropic-version": "2023-06-01"}, req, &resp)
	if err != nil {
		return Response{}, err
	}
	out := Response{
		Message: Message{Role: "assistant"},
		Usage:   Usage{In: resp.Usage.InputTokens, Out: resp.Usage.OutputTokens},
		Stop:    resp.StopReason,
	}
	for _, b := range resp.Content {
		switch b.Type {
		case "text":
			out.Message.Content += b.Text
		case "tool_use":
			out.Message.ToolCalls = append(out.Message.ToolCalls, ToolCall{ID: b.ID, Name: b.Name, Arguments: objectOrEmpty(b.Input)})
		}
	}
	return out, nil
}
