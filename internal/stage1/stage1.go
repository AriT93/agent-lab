// Package stage1 interprets requests with one LLM call that is constrained
// to return JSON matching a schema ("structured output").
//
// Lesson: the LLM is a *parser* here, not an agent. Our code still decides
// everything else; the model only fills in a typed form. Compare this with the
// original project, which asked for "category=x&type=y" as free text and then
// fed that text to a keyword matcher, so the blacklist was silently lost.
// With a schema, the model can't return anything we can't decode.
//
// There are two backends with the same prompt and schema: a local model via
// Ollama (ollama.go) and the Claude API (claude.go). Run both with -trace on
// the same request to compare them.
package stage1

const systemPrompt = `You translate a user's joke request into filters for JokeAPI.

- categories: only the categories the user asked for or clearly implied; empty means any.
- type: "single" for one-liners, "twopart" for setup/punchline, otherwise "any".
- blacklist: content the user wants excluded. "Clean", "safe" or "family friendly"
  means blacklist every flag. If the user says something is fine (e.g. "it can be
  dirty"), do not blacklist it.
- contains: JokeAPI matches this as a literal substring of the joke text, so it
  rarely matches multi-word topics. Use a single short word only when the user
  asks for a specific subject; otherwise use "".`
