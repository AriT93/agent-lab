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

- One package per stage (`internal/stageN`). Stages are meant to be read side
  by side, so some duplication between them (e.g. the agent loop) is
  intentional. Don't factor it out.
- Each stage file opens with a doc comment stating its lesson. Keep it accurate.
- Talk to model servers over plain HTTP with structs that mirror the wire format
  (`internal/ollama`). Don't swap in an SDK or framework except in a stage
  whose lesson is that framework (3b: langchaingo).
- API clients take typed requests, never free text. Model output is decoded and
  passed through `Normalize()` before use.
- Tool errors go back to the model as tool results, not Go errors, so it can retry.
- `-trace` should show every prompt, tool call, tool result, and token count.
  New stages should trace at least as much.
- Tests use the standard library with `httptest` fakes for every API and model;
  no network in `go test ./...`. Live tests are gated on `LIVE=1`.
- Run `gofmt`, `go vet ./...` and `go test ./...` before committing.

## Roadmap

See the stage table in README.md. Done: 0 (keywords), 1 (structured output),
2 (tool-calling loop), 3 (multi-tool + memory + trimming). Next: 3b (stage 3 in
langchaingo), 4 (MCP server for the joke tools, usable from Claude Code),
5 (evals), then provider switching.

Known weaknesses worth turning into eval cases: "clean" doesn't expand to a full
blacklist in stage 1; after an "explain it" turn, stage 3 explains later jokes
unasked (and sometimes wrongly).

## Commands

```bash
make test                                # unit tests, no network
make live                                # also hits real joke APIs
go run ./cmd/jokes -stage 3 -trace       # needs local Ollama
```
