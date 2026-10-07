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
make run                            # stage 3 agent with tracing on
go run ./cmd/jokes -stage 3b -trace # same agent in langchaingo
go run ./cmd/jokes -stage 6 -provider claude -trace  # same agent, any provider
go run ./cmd/jokes -stage 2 -trace  # single-tool agent
go run ./cmd/jokes -stage 1 -trace  # structured output
go run ./cmd/jokes -stage 0 -trace  # keyword baseline, no LLM
```

Stage 1 can also use the Claude API (`-backend claude`, needs `ANTHROPIC_API_KEY`
from a Console account; a Claude Pro subscription doesn't include API access).

### MCP server (stage 4)

```bash
claude mcp add jokes -- go run ./cmd/jokes-mcp   # then ask Claude Code for a joke
go run ./cmd/jokes-mcp -trace                    # logs every JSON-RPC message to stderr
```

### Web UI (stage 7)

```bash
go run ./cmd/jokes-web   # then open http://localhost:8080
```

Chat with the stage 3 agent in a browser (Gin). Each browser gets its own
conversation; every reply has a collapsible trace. Binds to localhost only
because there is no auth.

### LangGraph (stage 8, Python)

The same agent as a LangGraph graph, served the standard way: `langgraph dev`
exposes it over LangGraph's Agent Server API (threads, streaming, checkpointing)
and LangChain's open-source [agent-chat-ui](https://github.com/langchain-ai/agent-chat-ui)
is the browser front end. Needs `uv`, Node with pnpm, and local Ollama.

```bash
make lg-dev    # terminal 1: agent server on :2024
make lg-ui     # terminal 2: chat UI on :3000 (clones agent-chat-ui into .cache/)
make lg-chat   # or: terminal REPL with -trace style output, no UI
make py-test   # unit tests, no Ollama or network
```

### Ruby version

`ruby/` has stages 0-5 and the web UI again in plain Ruby, with a Sinatra chat
page instead of Gin, plus stage 3b in langchainrb. See [ruby/README.md](ruby/README.md).

```bash
make rb-test   # unit tests
make rb-run    # stage 3 REPL with tracing
make rb-web    # Sinatra UI on :8080
```

### Containers and deployment (stages 9 and 10)

`compose.yaml` runs the Go, Ruby and LangGraph apps and the LangGraph chat UI
in containers. The model stays on your Mac, because Docker there cannot use the
GPU. Stage 10 is a written guide to deploying, in [docs/deploy.md](docs/deploy.md).

```bash
make docker-up      # go :8080, ruby :8081, langgraph :2024, chat UI :3000
make docker-smoke   # health checks only, no model needed
make docker-down
```

### Evals (stage 5)

```bash
make eval                                          # stage 0 baseline, no LLM
go run ./cmd/jokes-eval -stage 1 -n 3 -trace       # 3 runs per case → pass rates
go run ./cmd/jokes-eval -stage 3 -run restriction  # agent conversations matching a regexp
go run ./cmd/jokes-eval -stage 6 -provider claude  # same cases, another model
```

### Providers (stage 6)

`-provider ollama` (default), `openai` (`OPENAI_API_KEY`; `OPENAI_BASE_URL` for
any compatible server) or `claude` (`ANTHROPIC_API_KEY`, a Console key). `-model`
overrides each provider's default. Memory limits below apply to Ollama only.

### Memory limits

Local models are memory hungry, so every Ollama request carries limits
(see `internal/ollama`):

| Flag | Default | What it controls |
|---|---|---|
| `-ctx` | 8192 | Context window. Some models load with 256K by default, which can push a 16 GB Mac into swap (we saw 112 s vs 7 s for the same request). |
| `-keep-alive` | 2m | How long the model stays loaded when idle. `0s` unloads after every call. |
| `-model` | `qwen3.5:9b-mlx` | The weights are the floor: ~9 GB here. `qwen3.5:4b` is roughly half. |

Stage 3b is the exception: langchaingo talks to Ollama's OpenAI-compatible
`/v1` endpoint, which has no `num_ctx` or `keep_alive`, so `-ctx` and
`-keep-alive` don't apply there (the output cap does). Use a model whose
Modelfile sets `num_ctx`, or set it server-side.

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
| 3 | **Multiple tools**, **conversation memory**, context trimming | `internal/stage3` | ✅ |
| 3b | Stage 3 again in langchaingo, to see what a framework hides | `internal/stage3lc` | ✅ |
| 4 | **MCP**: the joke tools as an MCP server (usable from Claude Code) | `internal/stage4`, `cmd/jokes-mcp` | ✅ |
| 5 | **Evals**: score each stage against a table of prompts and conversations | `internal/evals`, `cmd/jokes-eval` | ✅ |
| 6 | **Provider switching**: stage 3 on Ollama, OpenAI or Claude behind one interface | `internal/provider`, `internal/stage6` | ✅ |
| 7 | **Agent in a web app**: a Gin chat UI, one in-process agent per browser session | `internal/web`, `cmd/jokes-web` | ✅ |
| 8 | **LangGraph**: the agent as a graph (state, nodes, edges, checkpointer), served by the LangGraph dev server + agent-chat-ui | `python/` | ✅ |
| 9 | **Containers**: each web version in a small image, run together with Compose | `docker/`, `compose.yaml` | ✅ |
| 10 | **Deployment**: what changes on a server, with two options (hosted model API, or your own machine) | `docs/deploy.md` | 📄 guide only |

### Things to try

- Run the same prompt through `-stage 0` and `-stage 1` with `-trace`.
  Good ones: `keep it family friendly`, `nothing about politics or religion`,
  `a one-liner about computers`.
- Ask for `a joke about the chicago cubs, it can be dirty but not racist`.
  Stage 1 maps the topic to a `contains` filter, finds nothing, and fails.
  Stage 2's agent sees the "no matching joke" tool result and retries
  (`Cubs` → `Chicago` → no filter) while keeping the blacklist.
- Run stage 2 with and without `-think` and compare the steps it takes.
- In stage 3, hold a conversation: `a joke about dogs`, `another one`,
  `explain that one`, `from now on nothing political. a programming joke`.
  Watch which tool it picks, when it skips tools entirely, and the
  "turns in memory" count. `/reset` forgets everything.
- Run stage 3 with `-ctx 2048` to force history trimming early, then check
  whether a restriction from an early turn survives. (Often it doesn't; that's
  the lesson.)
- Run `-stage 3b` and compare its `-trace` with stage 3's: tools take one
  free-text string, history is pasted into the system prompt, and a tool error
  would abort the run if we let it. Then see what the framework saved you.
- Register `jokes-mcp` with Claude Code and ask for a clean dad joke about dogs.
  Claude, not our code, picks the tool and runs the loop.
- Run `jokes-eval -stage 0`, then `-stage 1`. Improve a prompt and watch which
  cases flip. The known weaknesses (`clean`, `no-unasked-explanations`) are
  cases on purpose.
- Run the same eval on `-provider ollama` and `-provider claude` (needs a key).
- Change a system prompt or `jokeapi.Schema()` and watch the trace.

## Layout

```
cmd/jokes/          REPL; picks a stage with -stage
cmd/jokes-mcp/      stage 4: MCP server on stdio
cmd/jokes-eval/     stage 5: eval runner
internal/jokeapi/   typed JokeAPI client (no NLP) + the Request schema
internal/dadjoke/   icanhazdadjoke.com client (second joke source)
internal/ollama/    raw-HTTP Ollama chat client with memory limits
internal/stage0/    keyword interpreter
internal/stage1/    structured-output interpreter (ollama.go, claude.go)
internal/stage2/    tool-calling agent loop
internal/stage3/    multi-tool agent with memory (tools.go: the tool registry)
internal/stage3lc/  stage 3 in langchaingo
internal/stage4/    MCP server (hand-written JSON-RPC) + the shared joke tools
internal/evals/     eval cases, checks and runner
internal/provider/  Ollama / OpenAI / Claude behind one interface (raw HTTP)
internal/stage6/    stage 3's agent over provider.Provider
internal/trace/     step printer used by -trace
```

## Tests

```bash
make test   # unit tests; fakes for every API and model, no network
make live   # also hits the real JokeAPI
```

The LLM tests run against `httptest` servers that pretend to be the model.
Stage 3b's fake speaks the OpenAI wire format, the provider tests check each
vendor's request shape (argument encoding, tool-result placement), and the MCP
tests drive the server with raw JSON-RPC lines. Stage 2's fake replays a scripted conversation (tool call → no match → retry →
answer), so the loop logic is tested without loading a model.

## License

MIT. See [LICENSE](LICENSE).
