// Package evals scores the stages against a fixed table of prompts.
//
// Lesson: "it seemed to work when I tried it" doesn't survive a prompt change
// or a model swap. An eval turns the behaviour you care about into cases with
// a pass/fail check, so you can change a prompt, a schema or a model and see
// what moved. Two kinds of case:
//
//   - Request cases (stages 0–1): one prompt → the jokeapi.Request an
//     interpreter produced. Checked on the typed request, not on text.
//   - Conversation cases (stage 3): several user messages in a row. Checked on
//     which tools the agent called and with what, plus the reply text.
//
// Checks are plain Go functions returning "" for a pass or a reason for a
// fail. Model output varies from run to run, so use -n to repeat each case and
// read the pass rate rather than trusting one run.
package evals

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/AriT93/agent-lab/internal/jokeapi"
)

// Interpreter is what stages 0 and 1 implement.
type Interpreter interface {
	Interpret(ctx context.Context, text string) (jokeapi.Request, error)
}

// Check returns "" if the value is acceptable, otherwise why not.
type Check[T any] func(T) string

// RequestCase is one prompt for an Interpreter.
type RequestCase struct {
	Name   string
	Prompt string
	Checks []Check[jokeapi.Request]
}

// Call is one tool call the agent made.
type Call struct {
	Tool   string
	Args   string
	Result string
}

// Turn is one user message in a conversation and what came of it.
type Turn struct {
	Say    string
	Checks []Check[Outcome]
}

// Outcome is what the agent did in response to one Turn.
type Outcome struct {
	Reply string
	Calls []Call
}

// ConversationCase is a scripted multi-turn chat with an Agent.
type ConversationCase struct {
	Name  string
	Turns []Turn
}

// Agent is what stage 3 (and later agents) implement for evals. Calls made
// while answering must be reported through the recorder given at construction.
type Agent interface {
	Respond(ctx context.Context, text string) (string, error)
}

// AgentFactory builds a fresh agent (empty memory) that reports its tool calls
// to record.
type AgentFactory func(record func(Call)) Agent

// Result is the verdict for one case in one run.
type Result struct {
	Name     string
	Failures []string // empty means pass
	Err      error    // the stage itself failed (network, bad model output)
	Took     time.Duration
}

func (r Result) Passed() bool { return r.Err == nil && len(r.Failures) == 0 }

// RunRequests runs every case whose name matches filter (nil = all).
func RunRequests(ctx context.Context, in Interpreter, cases []RequestCase, filter *regexp.Regexp) []Result {
	var out []Result
	for _, c := range cases {
		if filter != nil && !filter.MatchString(c.Name) {
			continue
		}
		start := time.Now()
		res := Result{Name: c.Name}
		req, err := in.Interpret(ctx, c.Prompt)
		if err != nil {
			res.Err = err
		} else {
			for _, check := range c.Checks {
				if why := check(req); why != "" {
					res.Failures = append(res.Failures, why)
				}
			}
		}
		res.Took = time.Since(start)
		out = append(out, res)
	}
	return out
}

// RunConversations runs every case whose name matches filter (nil = all), each
// on a fresh agent.
func RunConversations(ctx context.Context, newAgent AgentFactory, cases []ConversationCase, filter *regexp.Regexp) []Result {
	var out []Result
	for _, c := range cases {
		if filter != nil && !filter.MatchString(c.Name) {
			continue
		}
		start := time.Now()
		res := Result{Name: c.Name}
		var calls []Call
		agent := newAgent(func(call Call) { calls = append(calls, call) })
		for i, turn := range c.Turns {
			calls = nil
			reply, err := agent.Respond(ctx, turn.Say)
			if err != nil {
				res.Err = fmt.Errorf("turn %d (%q): %w", i+1, turn.Say, err)
				break
			}
			o := Outcome{Reply: reply, Calls: calls}
			for _, check := range turn.Checks {
				if why := check(o); why != "" {
					res.Failures = append(res.Failures, fmt.Sprintf("turn %d (%q): %s", i+1, turn.Say, why))
				}
			}
		}
		res.Took = time.Since(start)
		out = append(out, res)
	}
	return out
}

// Report prints one line per result and a total, and returns the pass count.
func Report(w io.Writer, results []Result) (passed int) {
	for _, r := range results {
		mark := "PASS"
		switch {
		case r.Err != nil:
			mark = "ERR "
		case !r.Passed():
			mark = "FAIL"
		}
		if r.Passed() {
			passed++
		}
		fmt.Fprintf(w, "%s  %-34s %6s\n", mark, r.Name, r.Took.Round(time.Millisecond))
		if r.Err != nil {
			fmt.Fprintf(w, "      error: %v\n", r.Err)
		}
		for _, f := range r.Failures {
			fmt.Fprintf(w, "      - %s\n", f)
		}
	}
	fmt.Fprintf(w, "\n%d/%d passed\n", passed, len(results))
	return passed
}

// Tally sums repeated runs of the same cases into pass counts per case name.
func Tally(runs [][]Result) (names []string, passes map[string]int) {
	passes = map[string]int{}
	for i, run := range runs {
		for _, r := range run {
			if i == 0 {
				names = append(names, r.Name)
			}
			if r.Passed() {
				passes[r.Name]++
			}
		}
	}
	return names, passes
}

// ---- checks ----

// HasCategories passes when the request names exactly these categories.
func HasCategories(want ...jokeapi.Category) Check[jokeapi.Request] {
	return func(r jokeapi.Request) string {
		if len(r.Categories) != len(want) {
			return fmt.Sprintf("categories = %v, want %v", r.Categories, want)
		}
		for _, w := range want {
			found := false
			for _, c := range r.Categories {
				found = found || c == w
			}
			if !found {
				return fmt.Sprintf("categories = %v, want %v", r.Categories, want)
			}
		}
		return ""
	}
}

// HasType passes when the request's joke type is want.
func HasType(want string) Check[jokeapi.Request] {
	return func(r jokeapi.Request) string {
		if r.Type != want {
			return fmt.Sprintf("type = %q, want %q", r.Type, want)
		}
		return ""
	}
}

// Blacklists passes when every flag in want is blacklisted.
func Blacklists(want ...jokeapi.Flag) Check[jokeapi.Request] {
	return func(r jokeapi.Request) string {
		var missing []jokeapi.Flag
		for _, w := range want {
			if !hasFlag(r.Blacklist, w) {
				missing = append(missing, w)
			}
		}
		if len(missing) > 0 {
			return fmt.Sprintf("blacklist = %v, missing %v", r.Blacklist, missing)
		}
		return ""
	}
}

// BlacklistsAll passes when every JokeAPI flag is blacklisted ("clean").
func BlacklistsAll() Check[jokeapi.Request] { return Blacklists(jokeapi.Flags...) }

// Allows passes when none of these flags is blacklisted.
func Allows(flags ...jokeapi.Flag) Check[jokeapi.Request] {
	return func(r jokeapi.Request) string {
		for _, f := range flags {
			if hasFlag(r.Blacklist, f) {
				return fmt.Sprintf("blacklist = %v, but the user allowed %q", r.Blacklist, f)
			}
		}
		return ""
	}
}

// NoBlacklist passes when nothing is blacklisted.
func NoBlacklist() Check[jokeapi.Request] {
	return func(r jokeapi.Request) string {
		if len(r.Blacklist) > 0 {
			return fmt.Sprintf("blacklist = %v, want none", r.Blacklist)
		}
		return ""
	}
}

// ContainsWord passes when the request filters on exactly one word, any of want.
func ContainsWord(want ...string) Check[jokeapi.Request] {
	return func(r jokeapi.Request) string {
		got := strings.ToLower(strings.TrimSpace(r.Contains))
		for _, w := range want {
			if got == strings.ToLower(w) {
				return ""
			}
		}
		return fmt.Sprintf("contains = %q, want one of %v", r.Contains, want)
	}
}

// NoContains passes when the request has no text filter.
func NoContains() Check[jokeapi.Request] {
	return func(r jokeapi.Request) string {
		if r.Contains != "" {
			return fmt.Sprintf("contains = %q, want none", r.Contains)
		}
		return ""
	}
}

func hasFlag(flags []jokeapi.Flag, f jokeapi.Flag) bool {
	for _, x := range flags {
		if x == f {
			return true
		}
	}
	return false
}

// UsesTool passes when the turn called the named tool at least once.
func UsesTool(name string) Check[Outcome] {
	return func(o Outcome) string {
		for _, c := range o.Calls {
			if c.Tool == name {
				return ""
			}
		}
		return fmt.Sprintf("never called %s (calls: %s)", name, toolNames(o.Calls))
	}
}

// NoTools passes when the turn answered without calling any tool.
func NoTools() Check[Outcome] {
	return func(o Outcome) string {
		if len(o.Calls) > 0 {
			return fmt.Sprintf("called tools (%s), expected an answer from memory", toolNames(o.Calls))
		}
		return ""
	}
}

// EveryCall passes when check holds for the arguments of each call to tool.
// Use it for restrictions that must never be dropped, even on retries.
func EveryCall(tool string, check Check[jokeapi.Request]) Check[Outcome] {
	return func(o Outcome) string {
		for _, c := range o.Calls {
			if c.Tool != tool {
				continue
			}
			var req jokeapi.Request
			if err := jsonUnmarshal(c.Args, &req); err != nil {
				return fmt.Sprintf("%s arguments are not a request: %v", tool, err)
			}
			if why := check(req.Normalize()); why != "" {
				return fmt.Sprintf("%s(%s): %s", tool, c.Args, why)
			}
		}
		return ""
	}
}

// ReplyContainsToolJoke passes when the reply includes the joke text from the
// last successful tool result (the agent must repeat jokes verbatim).
func ReplyContainsToolJoke() Check[Outcome] {
	return func(o Outcome) string {
		joke := ""
		for _, c := range o.Calls {
			if j := jokeFromResult(c.Result); j != "" {
				joke = j
			}
		}
		if joke == "" {
			return "no tool returned a joke"
		}
		if !strings.Contains(o.Reply, joke) {
			return fmt.Sprintf("reply does not contain the tool's joke %q", joke)
		}
		return ""
	}
}

// ShortReply passes when the reply is at most n characters longer than the
// joke it tells. An agent that explains a joke nobody asked about fails this.
func ShortReply(n int) Check[Outcome] {
	return func(o Outcome) string {
		joke := ""
		for _, c := range o.Calls {
			if j := jokeFromResult(c.Result); j != "" {
				joke = j
			}
		}
		if len(o.Reply) > len(joke)+n {
			return fmt.Sprintf("reply is %d chars for a %d-char joke: explained unasked?", len(o.Reply), len(joke))
		}
		return ""
	}
}

func toolNames(calls []Call) string {
	if len(calls) == 0 {
		return "none"
	}
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Tool
	}
	return strings.Join(names, ", ")
}

// UsesAnyTool passes when the turn called at least one of the named tools.
func UsesAnyTool(names ...string) Check[Outcome] {
	return func(o Outcome) string {
		for _, c := range o.Calls {
			for _, n := range names {
				if c.Tool == n {
					return ""
				}
			}
		}
		return fmt.Sprintf("expected one of %v, calls: %s", names, toolNames(o.Calls))
	}
}
