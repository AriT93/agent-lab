package evals

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/stage0"
)

type fixed jokeapi.Request

func (f fixed) Interpret(context.Context, string) (jokeapi.Request, error) {
	return jokeapi.Request(f), nil
}

type failing struct{}

func (failing) Interpret(context.Context, string) (jokeapi.Request, error) {
	return jokeapi.Request{}, errors.New("model offline")
}

func TestRunRequestsReportsReasons(t *testing.T) {
	cases := []RequestCase{
		{"ok", "x", []Check[jokeapi.Request]{HasCategories(jokeapi.Misc), NoContains()}},
		{"bad", "x", []Check[jokeapi.Request]{HasCategories(jokeapi.Dark), BlacklistsAll()}},
	}
	res := RunRequests(context.Background(), fixed{Categories: []jokeapi.Category{jokeapi.Misc}}, cases, nil)
	if !res[0].Passed() {
		t.Errorf("ok failed: %v", res[0].Failures)
	}
	if res[1].Passed() || len(res[1].Failures) != 2 {
		t.Errorf("bad = %+v, want two failures", res[1])
	}

	res = RunRequests(context.Background(), failing{}, cases[:1], nil)
	if res[0].Err == nil || res[0].Passed() {
		t.Errorf("interpreter error should be ERR, got %+v", res[0])
	}

	res = RunRequests(context.Background(), fixed{}, cases, regexp.MustCompile("^ok$"))
	if len(res) != 1 || res[0].Name != "ok" {
		t.Errorf("filter ignored: %+v", res)
	}
}

// Pins the baseline the later stages are trying to beat. If stage 0 changes,
// update this deliberately.
func TestStage0Baseline(t *testing.T) {
	res := RunRequests(context.Background(), stage0.Interpreter{}, RequestCases, nil)
	got := map[string]bool{}
	for _, r := range res {
		got[r.Name] = r.Passed()
	}
	for name, want := range map[string]bool{
		"category": true, "one-liner": true, "two-part": true,
		"clean": false, "two-flags": false, "topic-word": false,
	} {
		if got[name] != want {
			t.Errorf("stage 0 %s: passed=%v, want %v", name, got[name], want)
		}
	}
}

type scripted struct {
	record  func(Call)
	replies []string
	calls   [][]Call
	i       int
}

func (s *scripted) Respond(context.Context, string) (string, error) {
	for _, c := range s.calls[s.i] {
		s.record(c)
	}
	s.i++
	return s.replies[s.i-1], nil
}

func TestRunConversations(t *testing.T) {
	joke := `{"joke":"Why did the chicken cross the road?","source":"JokeAPI"}`
	factory := func(record func(Call)) Agent {
		return &scripted{
			record:  record,
			replies: []string{"Why did the chicken cross the road?", "It's a long and tedious explanation " + strings.Repeat("x", 300)},
			calls: [][]Call{
				{{Tool: "get_joke", Args: `{"categories":[],"type":"","blacklist":["political"],"contains":""}`, Result: joke}},
				{{Tool: "get_joke", Args: `{"blacklist":[]}`, Result: joke}},
			},
		}
	}
	cases := []ConversationCase{{"c", []Turn{
		{"joke", []Check[Outcome]{UsesTool("get_joke"), ReplyContainsToolJoke(), EveryCall("get_joke", Blacklists(jokeapi.Political))}},
		{"again", []Check[Outcome]{ShortReply(50), EveryCall("get_joke", Blacklists(jokeapi.Political)), NoTools()}},
	}}}
	res := RunConversations(context.Background(), factory, cases, nil)[0]
	// Turn 1 passes; turn 2 fails three ways: long reply, dropped restriction, tool called.
	if len(res.Failures) != 3 {
		t.Fatalf("failures = %v", res.Failures)
	}
	for _, f := range res.Failures {
		if !strings.HasPrefix(f, `turn 2 ("again")`) {
			t.Errorf("failure attributed to wrong turn: %s", f)
		}
	}

	var out bytes.Buffer
	if n := Report(&out, []Result{res, {Name: "fine"}}); n != 1 || !strings.Contains(out.String(), "1/2 passed") {
		t.Errorf("report:\n%s", out.String())
	}
}

func TestTally(t *testing.T) {
	runs := [][]Result{
		{{Name: "a"}, {Name: "b", Failures: []string{"x"}}},
		{{Name: "a"}, {Name: "b"}},
	}
	names, passes := Tally(runs)
	if len(names) != 2 || passes["a"] != 2 || passes["b"] != 1 {
		t.Errorf("names=%v passes=%v", names, passes)
	}
}
