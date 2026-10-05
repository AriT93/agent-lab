// Package jokeapi is a small client for https://v2.jokeapi.dev.
//
// It deliberately knows nothing about natural language: callers hand it a
// typed Request. Turning "tell me a clean programming joke" into a Request is
// the job of an interpreter (see the stage packages), which is the part of the
// system we swap out as we add more LLM involvement.
package jokeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Category string

const (
	Programming Category = "Programming"
	Misc        Category = "Misc"
	Dark        Category = "Dark"
	Pun         Category = "Pun"
	Spooky      Category = "Spooky"
	Christmas   Category = "Christmas"
)

var Categories = []Category{Programming, Misc, Dark, Pun, Spooky, Christmas}

// Flag marks content a joke contains. Flags in Request.Blacklist are excluded.
type Flag string

const (
	NSFW      Flag = "nsfw"
	Religious Flag = "religious"
	Political Flag = "political"
	Racist    Flag = "racist"
	Sexist    Flag = "sexist"
	Explicit  Flag = "explicit"
)

var Flags = []Flag{NSFW, Religious, Political, Racist, Sexist, Explicit}

// Request describes which joke to fetch. The zero value means "any joke".
type Request struct {
	Categories []Category `json:"categories"`
	Type       string     `json:"type"` // "", "single" or "twopart"
	Blacklist  []Flag     `json:"blacklist"`
	Contains   string     `json:"contains"` // substring the joke text must contain
}

// URL builds the JokeAPI URL for r against base (e.g. DefaultBaseURL).
func (r Request) URL(base string) string {
	cats := "Any"
	if len(r.Categories) > 0 {
		names := make([]string, len(r.Categories))
		for i, c := range r.Categories {
			names[i] = string(c)
		}
		cats = strings.Join(names, ",")
	}

	q := url.Values{}
	if r.Type == "single" || r.Type == "twopart" {
		q.Set("type", r.Type)
	}
	if len(r.Blacklist) > 0 {
		flags := make([]string, len(r.Blacklist))
		for i, f := range r.Blacklist {
			flags[i] = string(f)
		}
		q.Set("blacklistFlags", strings.Join(flags, ","))
	}
	if r.Contains != "" {
		q.Set("contains", r.Contains)
	}

	u := strings.TrimRight(base, "/") + "/" + cats
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// Joke is the subset of the JokeAPI response we use.
type Joke struct {
	ID       int             `json:"id"`
	Category string          `json:"category"`
	Type     string          `json:"type"`
	Joke     string          `json:"joke"`     // set when Type == "single"
	Setup    string          `json:"setup"`    // set when Type == "twopart"
	Delivery string          `json:"delivery"` // set when Type == "twopart"
	Flags    map[string]bool `json:"flags"`
	Safe     bool            `json:"safe"`
}

// Text renders the joke for display.
func (j Joke) Text() string {
	if j.Type == "twopart" {
		return j.Setup + "\n\n" + j.Delivery
	}
	return j.Joke
}

// APIError is returned when JokeAPI answers with "error": true, e.g. code 106
// "No matching joke found" when the filters are too narrow.
type APIError struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Causes  []string `json:"causedBy"`
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("jokeapi: %s (code %d)", e.Message, e.Code)
	if len(e.Causes) > 0 {
		msg += ": " + strings.Join(e.Causes, "; ")
	}
	return msg
}

const DefaultBaseURL = "https://v2.jokeapi.dev/joke"

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New() *Client {
	return &Client{BaseURL: DefaultBaseURL, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

func (c *Client) Fetch(ctx context.Context, r Request) (Joke, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL(c.BaseURL), nil)
	if err != nil {
		return Joke{}, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Joke{}, fmt.Errorf("jokeapi: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Joke{}, fmt.Errorf("jokeapi: reading body: %w", err)
	}

	// JokeAPI reports "no match" and bad filters as JSON with "error": true,
	// sometimes alongside a non-2xx status, so check the body first.
	var envelope struct {
		Error bool `json:"error"`
		APIError
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		if resp.StatusCode >= 300 {
			return Joke{}, fmt.Errorf("jokeapi: HTTP %d", resp.StatusCode)
		}
		return Joke{}, fmt.Errorf("jokeapi: decoding response: %w", err)
	}
	if envelope.Error {
		return Joke{}, &envelope.APIError
	}
	if resp.StatusCode >= 300 {
		return Joke{}, fmt.Errorf("jokeapi: HTTP %d", resp.StatusCode)
	}

	var j Joke
	if err := json.Unmarshal(body, &j); err != nil {
		return Joke{}, fmt.Errorf("jokeapi: decoding joke: %w", err)
	}
	if j.Type != "single" && j.Type != "twopart" {
		return Joke{}, fmt.Errorf("jokeapi: unknown joke type %q", j.Type)
	}
	return j, nil
}

// Schema is the JSON schema for a Request. LLM-facing code uses it both for
// structured output (stage 1) and as a tool's parameters (stage 2). Every
// property is required and extra properties are forbidden, as strict
// structured-output modes demand; "no preference" is "any", [] or "".
func Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"categories": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "enum": Categories},
				"description": "Categories to draw from; empty means any.",
			},
			"type": map[string]any{
				"type":        "string",
				"enum":        []string{"any", "single", "twopart"},
				"description": `"single" is a one-liner, "twopart" is setup + punchline.`,
			},
			"blacklist": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "enum": Flags},
				"description": "Content to exclude.",
			},
			"contains": map[string]any{
				"type":        "string",
				"description": "Literal substring the joke text must contain. Rarely matches multi-word phrases; use \"\" for none.",
			},
		},
		"required":             []string{"categories", "type", "blacklist", "contains"},
		"additionalProperties": false,
	}
}

// Normalize drops unknown and duplicate values and maps "any" to "". Models,
// especially small local ones, produce these even under a schema.
func (r Request) Normalize() Request {
	out := Request{Contains: strings.TrimSpace(r.Contains)}
	if r.Type == "single" || r.Type == "twopart" {
		out.Type = r.Type
	}
	seenC := map[Category]bool{}
	for _, c := range r.Categories {
		for _, known := range Categories {
			if strings.EqualFold(string(c), string(known)) && !seenC[known] {
				seenC[known] = true
				out.Categories = append(out.Categories, known)
			}
		}
	}
	seenF := map[Flag]bool{}
	for _, f := range r.Blacklist {
		for _, known := range Flags {
			if strings.EqualFold(string(f), string(known)) && !seenF[known] {
				seenF[known] = true
				out.Blacklist = append(out.Blacklist, known)
			}
		}
	}
	return out
}
