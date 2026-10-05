# agent-lab

A small Go project for learning how LLM apps and agents work, one concept at a
time. The domain is deliberately tiny: ask for a joke in plain English and get
one from [JokeAPI](https://v2.jokeapi.dev). Each stage swaps in a smarter
"interpreter" so you can compare approaches side by side.

This is the successor to `../simple`, a 2025 LangChain + aider experiment. The
lesson that carried over: **the joke client takes a typed `jokeapi.Request`,
never free text.** In the old app the LLM wrote `category=misc&blacklist=racist`
as a string and a keyword matcher re-parsed it, silently dropping the blacklist.

## Quick start

Runs on a local model through [Ollama](https://ollama.com); no API key needed.

```bash
ollama pull qwen3.5:9b-mlx          # default model (~9 GB in memory)
make run                            # stage 2 agent with tracing on
go run ./cmd/jokes -stage 1 -trace  # structured output
go run ./cmd/jokes -stage 0 -trace  # keyword baseline, no LLM
```

Stage 1 can also use the Claude API (`-backend claude`, needs `ANTHROPIC_API_KEY`
from a Console account; a Claude Pro subscription doesn't include API access).

### Memory limits

Local models are memory hungry, so every Ollama request carries limits
(see `internal/ollama`):

| Flag | Default | What it controls |
|---|---|---|
| `-ctx` | 8192 | Context window. Some models load with 256K by default, which can push a 16 GB Mac into swap (we saw 112 s vs 7 s for the same request). |
| `-keep-alive` | 2m | How long the model stays loaded when idle. `0s` unloads after every call. |
| `-model` | `qwen3.5:9b-mlx` | The weights are the floor: ~9 GB here. `qwen3.5:4b` is roughly half. |

Output is capped at 2048 tokens per call, and the REPL unloads the model when
it exits (including Ctrl-C). Check what's resident with `ollama ps`.

`-trace` prints every step (prompt, schema, model output, token usage, the
JokeAPI URL) to stderr. Most of the learning is in reading that output.

## Stages

| Stage | Concept | Package | Status |
|---|---|---|---|
| 0 | Baseline: keyword matching, no LLM | `internal/stage0` | ✅ |
| 1 | One LLM call with **structured output** (JSON schema); Ollama or Claude backend | `internal/stage1` | ✅ |
| 2 | **Tool calling** and a hand-written agent loop | `internal/stage2` | ✅ |
| 3 | Multiple tools + conversation memory; same thing again in langchaingo for comparison | `internal/stage3` | next |
| 4 | **MCP**: expose the joke client as an MCP server (usable from Claude Code) | `cmd/jokes-mcp` | |
| 5 | **Evals**: score each stage against a table of prompts → expected requests | `evals/` | |
| later | Provider switching (Claude / OpenAI / Ollama) behind one interface | | |

### Things to try

- Run the same prompt through `-stage 0` and `-stage 1` with `-trace`.
  Good ones: `keep it family friendly`, `nothing about politics or religion`,
  `a one-liner about computers`.
- Ask for `a joke about the chicago cubs, it can be dirty but not racist`.
  Stage 1 maps the topic to a `contains` filter, finds nothing, and fails.
  Stage 2's agent sees the "no matching joke" tool result and retries
  (`Cubs` → `Chicago` → no filter) while keeping the blacklist.
- Run stage 2 with and without `-think` and compare the steps it takes.
- Change a system prompt or `jokeapi.Schema()` and watch the trace.

## Layout

```
cmd/jokes/          REPL; picks a stage with -stage
internal/jokeapi/   typed JokeAPI client (no NLP) + the Request schema
internal/ollama/    raw-HTTP Ollama chat client with memory limits
internal/stage0/    keyword interpreter
internal/stage1/    structured-output interpreter (ollama.go, claude.go)
internal/stage2/    tool-calling agent loop
internal/trace/     step printer used by -trace
```

## Tests

```bash
make test   # unit tests; fakes for JokeAPI, Ollama and Claude, no network
make live   # also hits the real JokeAPI
```

The LLM tests run against `httptest` servers that pretend to be the model.
Stage 2's fake replays a scripted conversation (tool call → no match → retry →
answer), so the loop logic is tested without loading a model.

## License

MIT. See [LICENSE](LICENSE).
