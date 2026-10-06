package evals

import . "github.com/AriT93/agent-lab/internal/jokeapi"

type reqChecks = []Check[Request]

// RequestCases is the table for stages 0 and 1. Cases marked "known weakness"
// in CLAUDE.md are here on purpose: they should fail until the prompt or
// schema is improved, and then they guard the fix.
var RequestCases = []RequestCase{
	{"any-joke", "tell me a joke", reqChecks{NoBlacklist(), NoContains(), HasType("")}},
	{"category", "a programming joke", reqChecks{HasCategories(Programming)}},
	{"two-categories", "something dark or punny", reqChecks{HasCategories(Dark, Pun)}},
	{"no-topic-filter", "a spooky joke", reqChecks{HasCategories(Spooky), NoContains()}},
	{"one-liner", "give me a one-liner", reqChecks{HasType("single")}},
	{"two-part", "a two-part joke please", reqChecks{HasType("twopart")}},
	{"clean", "keep it clean", reqChecks{BlacklistsAll()}},
	{"family-friendly", "something family friendly", reqChecks{BlacklistsAll()}},
	{"clean-and-category", "a clean christmas joke", reqChecks{HasCategories(Christmas), BlacklistsAll()}},
	{"two-flags", "nothing about politics or religion", reqChecks{Blacklists(Political, Religious)}},
	{"allowed-flag", "a joke about computers, it can be dirty but not racist",
		reqChecks{Blacklists(Racist), Allows(NSFW, Explicit)}},
	{"topic-word", "a joke about dogs", reqChecks{ContainsWord("dog", "dogs")}},
	{"multiword-topic", "a joke about the chicago cubs", reqChecks{ContainsWord("cubs", "chicago", "cub")}},
}

type turnChecks = []Check[Outcome]

const (
	jokeTool = "get_joke"
	dadTool  = "search_dad_jokes"
)

// ConversationCases is the table for the agents (stage 3 and later).
var ConversationCases = []ConversationCase{
	{"picks-jokeapi-for-code", []Turn{
		{"a programming joke", turnChecks{UsesTool(jokeTool), ReplyContainsToolJoke()}},
	}},
	{"clean-expands-in-agent", []Turn{
		{"a clean programming joke", turnChecks{EveryCall(jokeTool, BlacklistsAll())}},
	}},
	{"another-one-uses-tool", []Turn{
		{"a joke about dogs", turnChecks{UsesAnyTool(jokeTool, dadTool)}},
		{"another one", turnChecks{UsesAnyTool(jokeTool, dadTool), ReplyContainsToolJoke()}},
	}},
	{"explain-needs-no-tool", []Turn{
		{"a joke about dogs", turnChecks{UsesAnyTool(jokeTool, dadTool)}},
		{"explain that joke", turnChecks{NoTools()}},
	}},
	// Known weakness: after an "explain it" turn the agent explains later jokes unasked.
	{"no-unasked-explanations", []Turn{
		{"a joke about dogs", turnChecks{UsesAnyTool(jokeTool, dadTool)}},
		{"explain that joke", turnChecks{NoTools()}},
		{"another one", turnChecks{UsesAnyTool(jokeTool, dadTool), ShortReply(150)}},
	}},
	{"restriction-persists", []Turn{
		{"from now on nothing political. a programming joke", turnChecks{EveryCall(jokeTool, Blacklists(Political))}},
		{"another one", turnChecks{UsesAnyTool(jokeTool, dadTool), EveryCall(jokeTool, Blacklists(Political))}},
	}},
}
