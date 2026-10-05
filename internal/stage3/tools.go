package stage3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/AriT93/agent-lab/internal/dadjoke"
	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/ollama"
)

// Tool pairs what the model sees (Def: name, description, JSON schema) with
// what our code does when the model calls it (Run). With one tool, stage 2
// could hard-code this; with several, a registry like this appears in every
// agent framework under some name.
type Tool struct {
	Def ollama.Tool
	Run func(ctx context.Context, args json.RawMessage) (any, error)
}

func function(name, description string, params any) ollama.Tool {
	return ollama.Tool{Type: "function", Function: ollama.ToolFunction{
		Name: name, Description: description, Parameters: params,
	}}
}

// seenJokes remembers which jokes were already told in this conversation.
// This is memory kept by *our code*, not the model: cheap, exact, and it
// survives history trimming.
type seenJokes map[string]bool

// maxRepeatRetries is how many times a tool re-fetches when it gets a joke
// the user has already heard.
const maxRepeatRetries = 3

var errOnlyRepeats = errors.New("only found jokes already told in this conversation; try different filters or the other tool")

func jokeAPITool(c *jokeapi.Client, seen seenJokes) Tool {
	return Tool{
		Def: function("get_joke",
			"Fetch a random joke from JokeAPI. Categories: Programming, Misc, Dark, Pun, Spooky, Christmas. "+
				"Supports content blacklists (nsfw, religious, political, racist, sexist, explicit). "+
				"Best for programming, dark, spooky or Christmas jokes, or when content filters matter.",
			jokeapi.Schema()),
		Run: func(ctx context.Context, args json.RawMessage) (any, error) {
			var req jokeapi.Request
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, fmt.Errorf("arguments are not valid JSON: %w", err)
			}
			req = req.Normalize()
			for range maxRepeatRetries {
				j, err := c.Fetch(ctx, req)
				if err != nil {
					return nil, err
				}
				key := fmt.Sprintf("jokeapi:%d", j.ID)
				if !seen[key] {
					seen[key] = true
					return map[string]any{"joke": j.Text(), "source": "JokeAPI", "category": j.Category}, nil
				}
			}
			return nil, errOnlyRepeats
		},
	}
}

func dadJokeTool(c *dadjoke.Client, seen seenJokes) Tool {
	return Tool{
		Def: function("search_dad_jokes",
			"Fetch a family-friendly dad joke from icanhazdadjoke.com. Searches joke text for one short word "+
				`(e.g. "dog", "ball", "pizza"); multi-word phrases rarely match. Use "" for a random joke.`,
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"term": map[string]any{"type": "string", "description": `One short search word, or "" for random.`},
				},
				"required":             []string{"term"},
				"additionalProperties": false,
			}),
		Run: func(ctx context.Context, args json.RawMessage) (any, error) {
			var in struct {
				Term string `json:"term"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("arguments are not valid JSON: %w", err)
			}
			for range maxRepeatRetries {
				var j dadjoke.Joke
				var err error
				if in.Term == "" {
					j, err = c.Random(ctx)
				} else {
					j, err = c.Search(ctx, in.Term)
				}
				if err != nil {
					return nil, err
				}
				if !seen["dad:"+j.ID] {
					seen["dad:"+j.ID] = true
					return map[string]any{"joke": j.Joke, "source": "icanhazdadjoke"}, nil
				}
			}
			return nil, errOnlyRepeats
		},
	}
}
