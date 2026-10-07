# agent-lab in Ruby

The same stages as the Go code in the repo root, written as plain, idiomatic
Ruby. If you know Ruby better than Go, read this directory first: the files
line up one-to-one, and each opens with a comment stating its lesson.

```bash
cd ruby
bundle install
bundle exec rake test                      # unit tests, no network, no Ollama
bundle exec bin/jokes --stage 3 --trace    # REPL; needs local Ollama
bundle exec bin/jokes --stage 3b --trace   # same agent in langchainrb
bundle exec bin/jokes-eval --stage 0       # evals; --stage 1/3 need a model
bundle exec bin/jokes-mcp --trace          # MCP server on stdio
bundle exec bin/jokes-web                  # Sinatra chat UI on http://localhost:8080
```

From the repo root the same things are `make rb-test`, `make rb-run`,
`make rb-web`, `make rb-eval`.

## Where each stage lives

| Stage | Concept | Ruby | Go equivalent |
|---|---|---|---|
| 0 | Keyword matching, no LLM | `lib/agent_lab/stage0.rb` | `internal/stage0` |
| 1 | Structured output (JSON schema) | `lib/agent_lab/stage1.rb` | `internal/stage1` |
| 2 | Tool calling, hand-written loop | `lib/agent_lab/stage2.rb` | `internal/stage2` |
| 3 | Two tools, memory, trimming | `lib/agent_lab/stage3.rb` | `internal/stage3` |
| 3b | Stage 3 again in langchainrb (`Langchain::Assistant`) | `lib/agent_lab/stage3b.rb` | `internal/stage3lc` |
| 4 | MCP server over stdio | `lib/agent_lab/stage4.rb`, `bin/jokes-mcp` | `internal/stage4` |
| 5 | Evals | `lib/agent_lab/evals*`, `bin/jokes-eval` | `internal/evals` |
| 7 | Web UI (Sinatra) | `lib/agent_lab/web.rb`, `views/` | `internal/web` (Gin) |

Supporting code: `ollama.rb` (the Ollama client; hashes mirror the wire
format), `joke_api.rb` and `dad_joke.rb` (the two joke APIs), `http.rb`
(Net::HTTP wrapper), `trace.rb` (what `--trace` prints).

Not ported, on purpose:

- **Stage 1's Claude backend and stage 6 (provider switching)** need API keys.
- **Stage 8 (LangGraph)** is a Python framework with no Ruby equivalent. Stage 3b
  (langchainrb) is the closest Ruby analogue of a "framework version" of the agent.
- The evals don't run against 3b: langchainrb names tools `joke_api__get_joke`,
  so the tool-name checks would need a mapping. A good exercise.

## Go to Ruby cheat sheet

| Go | Ruby here |
|---|---|
| `struct` with JSON tags | `Struct` (`JokeApi::Request`) or a plain `Hash` for wire data |
| `jokeapi.Request.Normalize()` | `JokeApi::Request#normalize` |
| `context.Context` | none; timeouts are on `Net::HTTP` |
| `error` return values | exceptions; tool errors are rescued and returned to the model as text |
| nil-safe `*trace.Tracer` | `Trace::OFF`, a tracer that prints nothing |
| `httptest` fakes | WebMock stubs (`test/test_helper.rb`) |
| `go test ./...` | `bundle exec rake test` (Minitest) |
| goroutine-per-request in `net/http` | Puma threads; one `Mutex` per conversation |
