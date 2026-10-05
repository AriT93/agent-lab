package stage0

import (
	"context"
	"reflect"
	"testing"

	"github.com/AriT93/agent-lab/internal/jokeapi"
)

func TestInterpret(t *testing.T) {
	tests := []struct {
		text string
		want jokeapi.Request
	}{
		{"tell me a joke", jokeapi.Request{}},
		{"a programming joke", jokeapi.Request{Categories: []jokeapi.Category{jokeapi.Programming}}},
		{"twopart christmas joke", jokeapi.Request{Type: "twopart", Categories: []jokeapi.Category{jokeapi.Christmas}}},
		{"it can be dirty but not racist", jokeapi.Request{Blacklist: []jokeapi.Flag{jokeapi.Racist}}},
		{"a clean pun", jokeapi.Request{Categories: []jokeapi.Category{jokeapi.Pun}, Blacklist: []jokeapi.Flag{jokeapi.NSFW}}},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got, err := Interpreter{}.Interpret(context.Background(), tt.text)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
