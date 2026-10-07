# python/ (stage 8: LangGraph)

Self-contained Python project: the stage 3 joke agent as a LangGraph graph.
Nothing here imports Go code, and no Go code imports this. Same owner setup and
memory limits as the root `CLAUDE.md` (local Ollama `qwen3.5:9b-mlx`, no API key,
16 GB Mac: keep `NUM_CTX`/`NUM_PREDICT`/`KEEP_ALIVE` in `agent_lab/graph.py`).

## Conventions

- Tooling is `uv` (Python >= 3.11). `pyproject.toml` and `uv.lock` are the source
  of truth; add dependencies with `uv add`.
- Stay close to standard LangGraph: `StateGraph`, `ToolNode`, `tools_condition`,
  reducers on state, checkpointer for memory, `langgraph dev` as the server,
  LangChain's agent-chat-ui as the front end. Prefer the framework's own
  mechanism over a hand-rolled one; the lesson is how LangGraph does it.
- Each module opens with a docstring stating what it teaches. Keep it accurate.
- Tool errors go back to the model as tool results, not exceptions.
- Per-conversation state (e.g. jokes already told) lives in graph `State`.
- Tests use pytest with a scripted fake model and `httpx.MockTransport` for the
  joke sites. No network, no Ollama.
- The graph is compiled without a checkpointer in `graph.py` because the dev
  server supplies one; the CLI passes its own.

## Commands (from the repo root)

```bash
make py-test    # unit tests
make lg-dev     # agent server on :2024
make lg-ui      # agent-chat-ui on :3000 (cloned into python/.cache/)
make lg-chat    # terminal REPL with trace output
```

If pnpm fails with "Cannot find matching keyid", `export COREPACK_INTEGRITY_KEYS=0`.
