"""Stage 8: the stage 3 agent as a LangGraph graph.

Stage 3 hand-wrote the loop (call the model, run its tool calls, repeat); 3b
hid it inside langchaingo's AgentExecutor. LangGraph makes it explicit as data:

    START → agent ──tool calls?──→ tools → agent …
                  └──no──→ END

  - State is a typed dict that every node reads and returns updates to
    (messages plus the jokes already told).
  - Nodes are plain functions. The "agent" node calls the model; "tools" is the
    stock ToolNode, which runs the calls the last message asked for.
  - The conditional edge (tools_condition) is the whole decision "is the model
    done?".
  - Memory is not code in the agent: a checkpointer saves the state after every
    step, keyed by thread id. `langgraph dev` supplies one, so this module
    compiles the graph without it. The CLI passes its own.

This module is what langgraph.json points at, so the dev server and the chat UI
get the compiled `graph` from here.
"""

import os

from langchain_core.messages import SystemMessage
from langchain_core.messages.utils import count_tokens_approximately, trim_messages
from langchain_ollama import ChatOllama
from langgraph.graph import END, START, StateGraph
from langgraph.prebuilt import ToolNode, tools_condition

from agent_lab.state import State
from agent_lab.tools import TOOLS

SYSTEM_PROMPT = """You are a joke assistant in an ongoing conversation. You get jokes only by calling tools; never write a joke yourself.

Choosing a tool:
- search_dad_jokes: clean dad jokes, searchable by one short word. Good for everyday topics
  (animals, food, sports) and when the user wants something family friendly.
- get_joke: JokeAPI. Good for programming, dark, pun, spooky or Christmas jokes, and when the
  user sets content limits. Turn "clean"/"family friendly" into a full blacklist.

If a tool finds nothing, try a shorter or related word, the other tool, or broader filters.
Never loosen content limits the user asked for, including ones from earlier in the
conversation. After 3 failed tool calls in a row, say you couldn't find one.

Use the conversation: "another one" means a new joke like the last request; questions about
a joke you told ("explain it", "why is that funny") need no tool.

When you tell a joke, give its text exactly as the tool returned it. If you changed the
request (other tool, broader filters), add one short sentence saying so."""

# Same memory limits as internal/ollama: a 16 GB Mac, so keep the context small
# and let the model unload itself when idle (there is no "unload on exit" hook
# in the dev server).
NUM_CTX = 8192
NUM_PREDICT = 2048
KEEP_ALIVE = "2m"
# Tokens of history to keep. The system prompt and tool schemas take ~1k of NUM_CTX,
# and the reply needs room. Like stage 3, this drops the oldest turns, so an early
# "nothing racist" can be trimmed away.
HISTORY_BUDGET = NUM_CTX * 3 // 4 - 1000

model = ChatOllama(
    model=os.environ.get("OLLAMA_MODEL", "qwen3.5:9b-mlx"),
    num_ctx=NUM_CTX,
    num_predict=NUM_PREDICT,
    keep_alive=KEEP_ALIVE,
    reasoning=False,
).bind_tools(TOOLS)


def agent(state: State) -> dict:
    history = trim_messages(
        state["messages"],
        strategy="last",
        max_tokens=HISTORY_BUDGET,
        token_counter=count_tokens_approximately,
        start_on="human",
    )
    return {"messages": [model.invoke([SystemMessage(SYSTEM_PROMPT), *history])]}


def build():
    """The uncompiled graph, so callers can choose a checkpointer."""
    g = StateGraph(State)
    g.add_node("agent", agent)
    g.add_node("tools", ToolNode(TOOLS))
    g.add_edge(START, "agent")
    g.add_conditional_edges("agent", tools_condition, {"tools": "tools", END: END})
    g.add_edge("tools", "agent")
    return g


# Six model calls per user message at most, like stage 3's MaxSteps. A step is
# one node run, so agent+tools pairs cost two.
graph = build().compile().with_config(recursion_limit=13)
