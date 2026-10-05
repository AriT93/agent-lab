// Package dadjoke is a small client for https://icanhazdadjoke.com, a second,
// always family-friendly joke source with word search.
package dadjoke

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"time"
)

type Joke struct {
	ID   string `json:"id"`
	Joke string `json:"joke"`
}

const DefaultBaseURL = "https://icanhazdadjoke.com"

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New() *Client {
	return &Client{BaseURL: DefaultBaseURL, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// ErrNoMatch means a search found nothing.
var ErrNoMatch = fmt.Errorf("dadjoke: no jokes matched the search term")

// Random returns one random joke.
func (c *Client) Random(ctx context.Context) (Joke, error) {
	var j Joke
	err := c.get(ctx, "/", &j)
	return j, err
}

// Search returns a random joke whose text matches term. The site matches word
// fragments ("ball" finds "balloon"), so short, common words work best.
func (c *Client) Search(ctx context.Context, term string) (Joke, error) {
	var page struct {
		Results []Joke `json:"results"`
	}
	if err := c.get(ctx, "/search?limit=30&term="+url.QueryEscape(term), &page); err != nil {
		return Joke{}, err
	}
	if len(page.Results) == 0 {
		return Joke{}, ErrNoMatch
	}
	return page.Results[rand.IntN(len(page.Results))], nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	// The API returns HTML unless asked for JSON, and asks clients to identify themselves.
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "agent-lab (https://github.com/AriT93/agent-lab)")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("dadjoke: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("dadjoke: HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("dadjoke: decoding response: %w", err)
	}
	return nil
}
