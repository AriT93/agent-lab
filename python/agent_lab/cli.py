"""A REPL for the graph: the Python twin of `go run ./cmd/jokes -stage 3`.

    uv run python -m agent_lab.cli --trace

Shows what the dev server does for you: compile the graph with a checkpointer
and call it with a thread id. Same thread id = same conversation.
"""

import argparse
import json
import uuid

from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
from langgraph.checkpoint.memory import InMemorySaver

from agent_lab import graph as g


def trace_update(node: str, update: dict) -> None:
    """Print one node's output; stream_mode="updates" yields one per node run."""
    for m in update.get("messages", []):
        if isinstance(m, AIMessage):
            u = m.usage_metadata or {}
            body = {"content": m.content, "tool_calls": [{"name": c["name"], "arguments": c["args"]} for c in m.tool_calls]}
            print(f"\033[2m── {node}: model ({u.get('input_tokens')} in / {u.get('output_tokens')} out tokens)\n{json.dumps(body, indent=2)}\033[0m")
        elif isinstance(m, ToolMessage):
            print(f"\033[2m── {node}: {m.name} result\n{m.content}\033[0m")


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--trace", action="store_true", help="print each model call, tool call and tool result")
    args = ap.parse_args()

    app = g.build().compile(checkpointer=InMemorySaver()).with_config(recursion_limit=13)
    thread = {"configurable": {"thread_id": str(uuid.uuid4())}}
    print(f"agent-lab stage 8 · LangGraph · {g.model.bound.model} · ctx {g.NUM_CTX}\n"
          "Ask for a joke; blank line or Ctrl-D to quit. /reset forgets the conversation.")
    while True:
        try:
            text = input("\n> ").strip()
        except EOFError:
            break
        if text in ("", "quit", "exit"):
            break
        if text == "/reset":
            thread = {"configurable": {"thread_id": str(uuid.uuid4())}}
            print("(conversation forgotten)")
            continue
        reply = ""
        try:
            for chunk in app.stream({"messages": [HumanMessage(text)]}, thread, stream_mode="updates"):
                for node, update in chunk.items():
                    if args.trace:
                        trace_update(node, update)
                    for m in update.get("messages", []):
                        if isinstance(m, AIMessage) and not m.tool_calls:
                            reply = m.content
        except Exception as e:  # keep the REPL alive, like the Go one
            print("error:", e)
            continue
        print(reply)


if __name__ == "__main__":
    main()
