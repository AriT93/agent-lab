"""The two joke tools, written the way LangGraph expects them.

Compared with the Go stages, the framework does more of the plumbing:

  - A tool is an ordinary typed function. @tool builds the JSON schema the model
    sees from the signature and docstring (Go: we wrote the schema by hand).
  - Arguments are validated against that schema before the function runs.
  - "Jokes already told" lives in the graph *state* (State.seen), not in a map
    owned by the agent object. A tool changes state by returning a Command, and
    the checkpointer persists it with the rest of the conversation.
  - Tool failures go back to the model as the tool result so it can retry,
    same as every other stage.
"""

import json
import random
from typing import Annotated, Literal

import httpx
from langchain_core.messages import ToolMessage
from langchain_core.tools import InjectedToolCallId, tool
from langgraph.prebuilt import InjectedState
from langgraph.types import Command

from agent_lab.state import State

JOKEAPI = "https://v2.jokeapi.dev/joke"
DADJOKE = "https://icanhazdadjoke.com"
MAX_REPEAT_RETRIES = 3
ERR_ONLY_REPEATS = "only found jokes already told in this conversation; try different filters or the other tool"

# Tests swap this for a client with a fake transport.
http = httpx.Client(timeout=10, headers={"User-Agent": "agent-lab (https://github.com/AriT93/agent-lab)"})

Category = Literal["Programming", "Misc", "Dark", "Pun", "Spooky", "Christmas"]
Flag = Literal["nsfw", "religious", "political", "racist", "sexist", "explicit"]


def _reply(call_id: str, payload: dict, seen_key: str | None = None) -> Command:
    update: dict = {"messages": [ToolMessage(json.dumps(payload), tool_call_id=call_id)]}
    if seen_key:
        update["seen"] = [seen_key]
    return Command(update=update)


def _error(call_id: str, message: str) -> Command:
    return _reply(call_id, {"error": message})


@tool
def get_joke(
    categories: list[Category],
    type: Literal["any", "single", "twopart"],
    blacklist: list[Flag],
    contains: str,
    state: Annotated[State, InjectedState],
    tool_call_id: Annotated[str, InjectedToolCallId],
) -> Command:
    """Fetch a random joke from JokeAPI. Best for programming, dark, spooky or Christmas
    jokes, or when content filters matter.

    Args:
        categories: Categories to draw from; empty means any.
        type: "single" is a one-liner, "twopart" is setup + punchline.
        blacklist: Content to exclude.
        contains: Literal substring the joke text must contain. Rarely matches multi-word phrases; use "" for none.
    """
    params = {}
    if type != "any":
        params["type"] = type
    if blacklist:
        params["blacklistFlags"] = ",".join(dict.fromkeys(blacklist))
    if contains.strip():
        params["contains"] = contains.strip()
    url = f"{JOKEAPI}/{','.join(dict.fromkeys(categories)) or 'Any'}"

    for _ in range(MAX_REPEAT_RETRIES):
        try:
            data = http.get(url, params=params).json()
        except (httpx.HTTPError, ValueError) as e:
            return _error(tool_call_id, f"jokeapi: {e}")
        if data.get("error"):
            return _error(tool_call_id, f"jokeapi: {data.get('message')} (code {data.get('code')}): {'; '.join(data.get('causedBy', []))}")
        key = f"jokeapi:{data['id']}"
        if key not in state.get("seen", []):
            text = f"{data['setup']}\n\n{data['delivery']}" if data["type"] == "twopart" else data["joke"]
            return _reply(tool_call_id, {"joke": text, "source": "JokeAPI", "category": data["category"]}, key)
    return _error(tool_call_id, ERR_ONLY_REPEATS)


@tool
def search_dad_jokes(
    term: str,
    state: Annotated[State, InjectedState],
    tool_call_id: Annotated[str, InjectedToolCallId],
) -> Command:
    """Fetch a family-friendly dad joke from icanhazdadjoke.com. Searches joke text for one
    short word (e.g. "dog", "ball", "pizza"); multi-word phrases rarely match.

    Args:
        term: One short search word, or "" for a random joke.
    """
    for _ in range(MAX_REPEAT_RETRIES):
        try:
            if term.strip():
                results = http.get(f"{DADJOKE}/search", params={"term": term.strip(), "limit": 30}, headers={"Accept": "application/json"}).json()["results"]
                if not results:
                    return _error(tool_call_id, "dadjoke: no jokes matched the search term")
                joke = random.choice(results)
            else:
                joke = http.get(DADJOKE, headers={"Accept": "application/json"}).json()
        except (httpx.HTTPError, ValueError, KeyError) as e:
            return _error(tool_call_id, f"dadjoke: {e}")
        key = f"dad:{joke['id']}"
        if key not in state.get("seen", []):
            return _reply(tool_call_id, {"joke": joke["joke"], "source": "icanhazdadjoke"}, key)
    return _error(tool_call_id, ERR_ONLY_REPEATS)


TOOLS = [get_joke, search_dad_jokes]
