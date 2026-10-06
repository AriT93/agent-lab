package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

const DefaultOpenAIModel = "gpt-4o-mini"

// OpenAI speaks the chat-completions API. BaseURL makes it work with any
// server that copies that API (including Ollama's /v1).
type OpenAI struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

// NewOpenAI reads OPENAI_API_KEY and OPENAI_BASE_URL (default https://api.openai.com/v1).
func NewOpenAI() *OpenAI {
	base := os.Getenv("OPENAI_BASE_URL")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	return &OpenAI{
		BaseURL: strings.TrimRight(base, "/"), APIKey: os.Getenv("OPENAI_API_KEY"),
		Model: DefaultOpenAIModel, HTTP: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (o *OpenAI) Name() string { return "openai/" + o.Model }

// Wire format.
type oaMessage struct {
	Role       string       `json:"role"`
	Content    string       `json:"content"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type oaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // a JSON object encoded as a string
	} `json:"function"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

type oaRequest struct {
	Model     string      `json:"model"`
	Messages  []oaMessage `json:"messages"`
	Tools     []oaTool    `json:"tools,omitempty"`
	MaxTokens int         `json:"max_tokens"`
}

type oaResponse struct {
	Choices []struct {
		Message      oaMessage `json:"message"`
		FinishReason string    `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (o *OpenAI) Chat(ctx context.Context, system string, messages []Message, tools []Tool) (Response, error) {
	req := oaRequest{Model: o.Model, MaxTokens: maxOutputTokens, Messages: []oaMessage{{Role: "system", Content: system}}}
	for _, m := range messages {
		om := oaMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolID}
		for _, c := range m.ToolCalls {
			tc := oaToolCall{ID: c.ID, Type: "function"}
			tc.Function.Name, tc.Function.Arguments = c.Name, string(objectOrEmpty(c.Arguments))
			om.ToolCalls = append(om.ToolCalls, tc)
		}
		req.Messages = append(req.Messages, om)
	}
	for _, t := range tools {
		ot := oaTool{Type: "function"}
		ot.Function.Name, ot.Function.Description, ot.Function.Parameters = t.Name, t.Description, t.Parameters
		req.Tools = append(req.Tools, ot)
	}

	var resp oaResponse
	err := postJSON(ctx, o.HTTP, "openai", o.BaseURL+"/chat/completions",
		map[string]string{"Authorization": "Bearer " + o.APIKey}, req, &resp)
	if err != nil {
		return Response{}, err
	}
	if len(resp.Choices) == 0 {
		return Response{}, errNoChoices
	}
	ch := resp.Choices[0]
	out := Response{
		Message: Message{Role: "assistant", Content: ch.Message.Content},
		Usage:   Usage{In: resp.Usage.PromptTokens, Out: resp.Usage.CompletionTokens},
		Stop:    ch.FinishReason,
	}
	for _, c := range ch.Message.ToolCalls {
		out.Message.ToolCalls = append(out.Message.ToolCalls, ToolCall{
			ID: c.ID, Name: c.Function.Name, Arguments: objectOrEmpty(json.RawMessage(c.Function.Arguments)),
		})
	}
	return out, nil
}
