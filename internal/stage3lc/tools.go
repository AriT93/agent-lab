package stage3lc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AriT93/agent-lab/internal/dadjoke"
	"github.com/AriT93/agent-lab/internal/jokeapi"
)

// Both tools implement langchaingo's tools.Tool: Name, Description and
// Call(ctx, string) (string, error). There is nowhere to put a JSON schema, so
// the "schema" is prose in the description, and Call gets whatever string the
// model wrote. Errors become {"error": ...} text so the model can retry.

const maxRepeatRetries = 3

const errOnlyRepeats = "only found jokes already told in this conversation; try different filters or the other tool"

type jokeAPITool struct {
	c    *jokeapi.Client
	seen map[string]bool
}

func (jokeAPITool) Name() string { return "get_joke" }

func (jokeAPITool) Description() string {
	schema, _ := json.Marshal(jokeapi.Schema())
	return "Fetch a random joke from JokeAPI. Categories: Programming, Misc, Dark, Pun, Spooky, Christmas. " +
		"Supports content blacklists (nsfw, religious, political, racist, sexist, explicit). " +
		"Best for programming, dark, spooky or Christmas jokes, or when content filters matter. " +
		"The input MUST be a JSON object (as a string) matching this JSON schema: " + string(schema)
}

func (t jokeAPITool) Call(ctx context.Context, input string) (string, error) {
	var req jokeapi.Request
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return toJSON(map[string]string{"error": "input is not a valid JSON object: " + err.Error()}), nil
	}
	req = req.Normalize()
	for range maxRepeatRetries {
		j, err := t.c.Fetch(ctx, req)
		if err != nil {
			return toJSON(map[string]string{"error": err.Error()}), nil
		}
		key := fmt.Sprintf("jokeapi:%d", j.ID)
		if !t.seen[key] {
			t.seen[key] = true
			return toJSON(map[string]any{"joke": j.Text(), "source": "JokeAPI", "category": j.Category}), nil
		}
	}
	return toJSON(map[string]string{"error": errOnlyRepeats}), nil
}

type dadJokeTool struct {
	c    *dadjoke.Client
	seen map[string]bool
}

func (dadJokeTool) Name() string { return "search_dad_jokes" }

func (dadJokeTool) Description() string {
	return "Fetch a family-friendly dad joke from icanhazdadjoke.com. The input is one short search word " +
		`(e.g. "dog", "ball", "pizza") as plain text; multi-word phrases rarely match. Use an empty string for a random joke.`
}

func (t dadJokeTool) Call(ctx context.Context, input string) (string, error) {
	term := strings.Trim(strings.TrimSpace(input), `"`)
	for range maxRepeatRetries {
		var j dadjoke.Joke
		var err error
		if term == "" {
			j, err = t.c.Random(ctx)
		} else {
			j, err = t.c.Search(ctx, term)
		}
		if err != nil {
			return toJSON(map[string]string{"error": err.Error()}), nil
		}
		if !t.seen["dad:"+j.ID] {
			t.seen["dad:"+j.ID] = true
			return toJSON(map[string]any{"joke": j.Joke, "source": "icanhazdadjoke"}), nil
		}
	}
	return toJSON(map[string]string{"error": errOnlyRepeats}), nil
}
