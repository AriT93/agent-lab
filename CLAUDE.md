# agent-lab

A learning project, not a product. Each stage teaches one LLM/agent concept
using a deliberately tiny domain (ask for a joke, fetch one from an API).
Clarity of each concept beats abstraction or cleverness.

## Owner's setup

- Claude Pro subscription only: **no Anthropic API key**. The main LLM backend
  is a local model through Ollama (`qwen3.5:9b-mlx`) on a 16 GB Mac.
- The Claude API backend (`internal/stage1/claude.go`) exists for comparison
  but can't run without a Console API key.
- Memory is tight. Keep the Ollama limits in `internal/ollama` (num_ctx 8192,
  num_predict 2048, keep_alive 2m, unload on exit) and don't raise defaults
  without saying why.
- Cloud sessions have no Ollama: `make test` works (everything is faked), live
  runs don't.

## Conventions

- One package per stage (`internal/stageN`; 3b is `stage3lc`). Stages are meant to be read side
  by side, so some duplication between them (e.g. the agent loop) is
  intentional. Don't factor it out.
- Each stage file opens with a doc comment stating its lesson. Keep it accurate.
- Talk to model servers over plain HTTP with structs that mirror the wire format
  (`internal/ollama`). Don't swap in an SDK or framework except in a stage
  whose lesson is that framework (3b: langchaingo; 7: Gin, for the web UI only).
- Stage 8 is Python (LangGraph) and lives entirely in `python/`; see `python/CLAUDE.md`.
  These Go conventions don't apply there.
- API clients take typed requests, never free text. Model output is decoded and
  passed through `Normalize()` before use.
- Tool errors go back to the model as tool results, not Go errors, so it can retry.
- `-trace` should show every prompt, tool call, tool result, and token count.
  New stages should trace at least as much.
- Tests use the standard library with `httptest` fakes for every API and model;
  no network in `go test ./...`. Live tests are gated on `LIVE=1`.
- Stage 4's tools (`internal/stage4`) are the one shared tool implementation:
  stage 6 reuses them. Stages 2–3b keep their own on purpose.
- Evals: checks look at typed requests and tool calls, not reply prose. Agents
  expose an `OnTool` hook for that. Model output varies, so use `-n`.
- Run `gofmt`, `go vet ./...` and `go test ./...` before committing.

## Roadmap

See the stage table in README.md. All planned stages are done: 0 (keywords),
1 (structured output), 2 (tool-calling loop), 3 (multi-tool + memory +
trimming), 3b (stage 3 in langchaingo), 4 (MCP server), 5 (evals),
6 (provider switching), 7 (web UI), 8 (LangGraph in Python, `python/`). Ideas if you continue: run the evals per provider and
record pass rates, fix the failing eval cases by changing prompts, add a
resource or prompt to the MCP server.

Known weaknesses, now eval cases (`internal/evals/cases.go`): "clean" doesn't
expand to a full blacklist in stage 1 (`clean`); after an "explain it" turn,
stage 3 explains later jokes unasked (`no-unasked-explanations`).

## Commands

```bash
make test                                # unit tests, no network
make live                                # also hits real joke APIs
go run ./cmd/jokes -stage 3 -trace       # needs local Ollama
go run ./cmd/jokes-eval -stage 0         # evals; -stage 1/3/6 need a model
go run ./cmd/jokes-mcp -trace            # MCP server on stdio
go run ./cmd/jokes-web                   # browser chat UI on :8080, needs local Ollama
make py-test                             # stage 8 (python/, uv) unit tests
make lg-dev / lg-ui / lg-chat            # stage 8 server, chat UI, terminal REPL
```
