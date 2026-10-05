// Ollama backend for stage 1.
package stage1

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/ollama"
	"github.com/AriT93/agent-lab/internal/trace"
)

// Ollama is the stage 1 interpreter backed by a local model.
type Ollama struct {
	Client *ollama.Client
	Trace  *trace.Tracer
}

func (in *Ollama) Interpret(ctx context.Context, text string) (jokeapi.Request, error) {
	think := false // a form-filling task doesn't need reasoning; thinking costs seconds locally
	req := ollama.ChatRequest{
		Messages: []ollama.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: text},
		},
		// Ollama turns the schema into a grammar, so the model can only emit
		// tokens that keep the JSON valid against it.
		Format: jokeapi.Schema(),
		Think:  &think,
	}
	in.Trace.Step("stage1: request to "+in.Client.Model, map[string]any{"system": systemPrompt, "user": text})

	resp, err := in.Client.Chat(ctx, req)
	if err != nil {
		return jokeapi.Request{}, err
	}
	in.Trace.Step("stage1: usage", usage(resp))
	in.Trace.Step("stage1: model output", resp.Message.Content)

	if resp.DoneReason == "length" {
		return jokeapi.Request{}, fmt.Errorf("model hit the output limit (num_predict) before finishing")
	}
	var r jokeapi.Request
	if err := json.Unmarshal([]byte(resp.Message.Content), &r); err != nil {
		return jokeapi.Request{}, fmt.Errorf("decoding model output: %w", err)
	}
	return r.Normalize(), nil
}

func usage(r ollama.ChatResponse) map[string]any {
	return map[string]any{
		"done_reason":   r.DoneReason,
		"input_tokens":  r.PromptEvalCount,
		"output_tokens": r.EvalCount,
		"total":         time.Duration(r.TotalDuration).Round(time.Millisecond).String(),
		"load":          time.Duration(r.LoadDuration).Round(time.Millisecond).String(),
	}
}
