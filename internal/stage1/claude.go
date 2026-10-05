// Claude backend for stage 1.
package stage1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"

	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/trace"
)

const DefaultClaudeModel = "claude-opus-5-5"

// Claude is the stage 1 interpreter backed by the Claude API.
type Claude struct {
	Client anthropic.Client
	Model  string
	Trace  *trace.Tracer
}

// NewClaude builds a Claude interpreter. With no options the client reads ANTHROPIC_API_KEY
// from the environment; tests pass option.WithBaseURL to point at a fake server.
func NewClaude(opts ...option.RequestOption) *Claude {
	return &Claude{Client: anthropic.NewClient(opts...), Model: DefaultClaudeModel}
}

func (in *Claude) Interpret(ctx context.Context, text string) (jokeapi.Request, error) {
	params := anthropic.BetaMessageNewParams{
		Model:     in.Model,
		MaxTokens: 16000,
		System:    []anthropic.BetaTextBlockParam{{Text: systemPrompt}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(text)),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{
			// Filling in a form is easy; low effort keeps it fast and cheap.
			Effort: anthropic.BetaOutputConfigEffortLow,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: jokeapi.Schema()},
		},
		// If a safety classifier declines the request, let the API retry it on
		// a fallback model it picks, instead of failing outright.
		Fallbacks: anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()},
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	}
	in.Trace.Step("stage1: request to "+in.Model, map[string]any{
		"system": systemPrompt, "user": text,
	})

	msg, err := in.Client.Beta.Messages.New(ctx, params)
	if err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			return jokeapi.Request{}, fmt.Errorf("claude API error %d: %w", apiErr.StatusCode, err)
		}
		return jokeapi.Request{}, err
	}
	in.Trace.Step("stage1: usage", map[string]any{
		"stop_reason":   msg.StopReason,
		"input_tokens":  msg.Usage.InputTokens,
		"output_tokens": msg.Usage.OutputTokens,
	})

	// Always check why the model stopped before trusting the content.
	switch msg.StopReason {
	case anthropic.BetaStopReasonEndTurn:
	case anthropic.BetaStopReasonRefusal:
		return jokeapi.Request{}, fmt.Errorf("model declined the request (%s)", msg.StopDetails.Category)
	default:
		return jokeapi.Request{}, fmt.Errorf("unexpected stop reason %q", msg.StopReason)
	}

	// The reply may include thinking blocks; the JSON is in the text block.
	var raw string
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			raw += t.Text
		}
	}
	in.Trace.Step("stage1: model output", raw)

	var r jokeapi.Request
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return jokeapi.Request{}, fmt.Errorf("decoding model output: %w", err)
	}
	return r.Normalize(), nil
}
