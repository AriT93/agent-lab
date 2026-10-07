# ruby/ (stages 0-5, 3b and 7 in Ruby)

A Ruby port of the Go stages, written for someone who knows Ruby better than
Go. Self-contained: nothing here imports Go or Python code. Same owner setup
and memory limits as the root `CLAUDE.md` (local Ollama `qwen3.5:9b-mlx`, no
API key, 16 GB Mac): keep the defaults in `lib/agent_lab/ollama.rb`
(num_ctx 8192, num_predict 2048, keep_alive 2m, unload on exit).

## Conventions

- Canonical, idiomatic Ruby (>= 3.1): `frozen_string_literal`, small classes,
  `Struct`s for value objects, exceptions for failures, keyword arguments.
  Prefer readability over cleverness; this code exists to explain the stages.
- Stdlib only for stages 0-5 (`net/http`, `json`, `optparse`). Gems are
  confined to the web UI (Sinatra + Puma), stage 3b (langchainrb, whose lesson is
  that framework) and tests.
- Mirror the Go structure: one file per stage under `lib/agent_lab/`, each
  opening with a comment stating its lesson. Duplication between stages is
  intentional. When the Go version changes behaviour (prompts, schemas, eval
  cases), change the Ruby one to match.
- Talk to Ollama over plain HTTP with hashes that mirror the wire format.
  Model output is decoded and passed through `Request#normalize` before use.
- Tool errors go back to the model as tool results, not exceptions.
- `--trace` shows every prompt, tool call, tool result and token count.
- Tests are Minitest + WebMock + rack-test with a scripted model
  (`script_model` in `test/test_helper.rb`); no network, no Ollama.
- Evals: a check is a lambda returning nil (pass) or a reason (String).
  Agents expose `on_tool` for that. Model output varies, so use `-n`.

## Commands (from `ruby/`, or `make rb-*` from the repo root)

```bash
bundle install
bundle exec rake test
bundle exec bin/jokes --stage 3 --trace
bundle exec bin/jokes-eval --stage 0
bundle exec bin/jokes-mcp --trace
bundle exec bin/jokes-web
```
