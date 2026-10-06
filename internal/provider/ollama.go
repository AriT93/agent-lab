package provider

import (
	"context"

	"github.com/AriT93/agent-lab/internal/ollama"
)

// Ollama adapts internal/ollama.Client, keeping its memory limits.
type Ollama struct {
	Client *ollama.Client
	Think  bool
}

func (o *Ollama) Name() string { return "ollama/" + o.Client.Model }

func (o *Ollama) Chat(ctx context.Context, system string, messages []Message, tools []Tool) (Response, error) {
	msgs := []ollama.Message{{Role: "system", Content: system}}
	for _, m := range messages {
		om := ollama.Message{Role: m.Role, Content: m.Content, ToolName: m.ToolName}
		for _, c := range m.ToolCalls {
			var tc ollama.ToolCall
			tc.ID, tc.Function.Name, tc.Function.Arguments = c.ID, c.Name, objectOrEmpty(c.Arguments)
			om.ToolCalls = append(om.ToolCalls, tc)
		}
		msgs = append(msgs, om)
	}
	var defs []ollama.Tool
	for _, t := range tools {
		defs = append(defs, ollama.Tool{Type: "function", Function: ollama.ToolFunction{
			Name: t.Name, Description: t.Description, Parameters: t.Parameters,
		}})
	}

	resp, err := o.Client.Chat(ctx, ollama.ChatRequest{Messages: msgs, Tools: defs, Think: &o.Think})
	if err != nil {
		return Response{}, err
	}
	out := Response{
		Message: Message{Role: "assistant", Content: resp.Message.Content},
		Usage:   Usage{In: resp.PromptEvalCount, Out: resp.EvalCount},
		Stop:    resp.DoneReason,
	}
	for _, c := range resp.Message.ToolCalls {
		out.Message.ToolCalls = append(out.Message.ToolCalls, ToolCall{
			ID: c.ID, Name: c.Function.Name, Arguments: objectOrEmpty(c.Function.Arguments),
		})
	}
	return out, nil
}
