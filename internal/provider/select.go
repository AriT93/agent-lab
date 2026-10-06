package provider

import (
	"fmt"

	"github.com/AriT93/agent-lab/internal/ollama"
)

// Names lists the values accepted by ByName.
const Names = "ollama, openai or claude"

// ByName picks a provider. model overrides the provider's default model; llm
// is the (already configured) Ollama client used for "ollama".
func ByName(name, model string, llm *ollama.Client, think bool) (Provider, error) {
	switch name {
	case "ollama":
		if model != "" {
			llm.Model = model
		}
		return &Ollama{Client: llm, Think: think}, nil
	case "openai":
		p := NewOpenAI()
		if model != "" {
			p.Model = model
		}
		// A custom base URL is usually a local server that needs no key.
		if p.APIKey == "" && p.BaseURL == "https://api.openai.com/v1" {
			return nil, fmt.Errorf("openai: OPENAI_API_KEY is not set")
		}
		return p, nil
	case "claude":
		p := NewClaude()
		if model != "" {
			p.Model = model
		}
		if p.APIKey == "" {
			return nil, fmt.Errorf("claude: ANTHROPIC_API_KEY is not set (needs a Console API key; a Claude Pro subscription doesn't include one)")
		}
		return p, nil
	}
	return nil, fmt.Errorf("unknown provider %q (want %s)", name, Names)
}
