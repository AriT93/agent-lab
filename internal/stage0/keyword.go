// Package stage0 interprets requests with plain keyword matching. No LLM.
//
// This is the baseline from the original "simple" project. It is fast, free
// and predictable, and it is also brittle: "keep it clean" or "no politics"
// slip straight past it. Every later stage is trying to beat this one.
package stage0

import (
	"context"
	"strings"

	"github.com/AriT93/agent-lab/internal/jokeapi"
)

type Interpreter struct{}

func (Interpreter) Interpret(_ context.Context, text string) (jokeapi.Request, error) {
	text = strings.ToLower(text)
	var r jokeapi.Request

	switch {
	case strings.Contains(text, "twopart"), strings.Contains(text, "two-part"):
		r.Type = "twopart"
	case strings.Contains(text, "single"), strings.Contains(text, "one-liner"):
		r.Type = "single"
	}

	for _, c := range jokeapi.Categories {
		if strings.Contains(text, strings.ToLower(string(c))) {
			r.Categories = append(r.Categories, c)
		}
	}

	for _, f := range jokeapi.Flags {
		if strings.Contains(text, "no "+string(f)) || strings.Contains(text, "not "+string(f)) {
			r.Blacklist = append(r.Blacklist, f)
		}
	}
	if strings.Contains(text, "clean") && !contains(r.Blacklist, jokeapi.NSFW) {
		r.Blacklist = append(r.Blacklist, jokeapi.NSFW)
	}

	return r, nil
}

func contains(flags []jokeapi.Flag, f jokeapi.Flag) bool {
	for _, x := range flags {
		if x == f {
			return true
		}
	}
	return false
}
