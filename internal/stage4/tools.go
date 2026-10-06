package stage4

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AriT93/agent-lab/internal/dadjoke"
	"github.com/AriT93/agent-lab/internal/jokeapi"
)

// NewServer returns the joke server. Unlike stage 3 there is no "seen jokes"
// memory: an MCP server doesn't know which conversation a call belongs to, so
// de-duplicating is the client's job.
func NewServer(jokes *jokeapi.Client, dads *dadjoke.Client) *Server {
	return &Server{
		Name:    "agent-lab-jokes",
		Version: "0.1.0",
		Tools:   []Tool{jokeAPITool(jokes), dadJokeTool(dads)},
	}
}

func jokeAPITool(c *jokeapi.Client) Tool {
	return Tool{
		Name: "get_joke",
		Description: "Fetch a random joke from JokeAPI. Categories: Programming, Misc, Dark, Pun, Spooky, Christmas. " +
			"Supports content blacklists (nsfw, religious, political, racist, sexist, explicit). " +
			"Best for programming, dark, spooky or Christmas jokes, or when content filters matter.",
		InputSchema: jokeapi.Schema(),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var req jokeapi.Request
			if err := json.Unmarshal(args, &req); err != nil {
				return "", fmt.Errorf("arguments are not valid JSON: %w", err)
			}
			j, err := c.Fetch(ctx, req.Normalize())
			if err != nil {
				return "", err
			}
			return j.Text(), nil
		},
	}
}

func dadJokeTool(c *dadjoke.Client) Tool {
	return Tool{
		Name: "search_dad_jokes",
		Description: "Fetch a family-friendly dad joke from icanhazdadjoke.com. Searches joke text for one short word " +
			`(e.g. "dog", "ball", "pizza"); multi-word phrases rarely match. Use "" for a random joke.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"term": map[string]any{"type": "string", "description": `One short search word, or "" for random.`},
			},
			"required":             []string{"term"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Term string `json:"term"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("arguments are not valid JSON: %w", err)
			}
			var j dadjoke.Joke
			var err error
			if in.Term == "" {
				j, err = c.Random(ctx)
			} else {
				j, err = c.Search(ctx, in.Term)
			}
			if err != nil {
				return "", err
			}
			return j.Joke, nil
		},
	}
}
