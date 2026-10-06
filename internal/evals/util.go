package evals

import "encoding/json"

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

// jokeFromResult extracts "joke" from a tool result like {"joke": "...", ...}.
func jokeFromResult(result string) string {
	var r struct {
		Joke string `json:"joke"`
	}
	if json.Unmarshal([]byte(result), &r) != nil {
		return ""
	}
	return r.Joke
}
