"""No network, no Ollama: the model is scripted and the joke sites are an httpx MockTransport."""

import httpx
import pytest
from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
from langgraph.checkpoint.memory import InMemorySaver

from agent_lab import graph as g
from agent_lab import tools


class ScriptedModel:
    """Stands in for the bound ChatOllama: replays canned replies, records prompts."""

    def __init__(self, *replies: AIMessage):
        self.replies, self.prompts = list(replies), []

    def invoke(self, messages):
        self.prompts.append(messages)
        return self.replies[len(self.prompts) - 1]


def call(name: str, **args) -> AIMessage:
    return AIMessage("", tool_calls=[{"name": name, "args": args, "id": f"c{name}"}])


@pytest.fixture(autouse=True)
def fake_sites(monkeypatch):
    def handler(req: httpx.Request) -> httpx.Response:
        if req.url.host == "v2.jokeapi.dev":
            return httpx.Response(200, json={"error": False, "type": "single", "joke": "The only JokeAPI joke.", "category": "Misc", "id": 1})
        if req.url.path == "/search" and req.url.params["term"] == "dog":
            return httpx.Response(200, json={"results": [{"id": "d1", "joke": "A dog joke."}]})
        if req.url.path == "/search":
            return httpx.Response(200, json={"results": []})
        return httpx.Response(200, json={"id": "r1", "joke": "A random dad joke."})

    monkeypatch.setattr(tools, "http", httpx.Client(transport=httpx.MockTransport(handler)))


def run(monkeypatch, *replies):
    model = ScriptedModel(*replies)
    monkeypatch.setattr(g, "model", model)
    app = g.build().compile(checkpointer=InMemorySaver())
    return app, model, {"configurable": {"thread_id": "t"}}


def test_tool_call_result_reaches_model_and_memory_persists(monkeypatch):
    app, model, cfg = run(monkeypatch, call("search_dad_jokes", term="dog"), AIMessage("A dog joke."), AIMessage("It's a pun."))

    out = app.invoke({"messages": [HumanMessage("a joke about dogs")]}, cfg)
    assert out["messages"][-1].content == "A dog joke."
    assert out["seen"] == ["dad:d1"]
    tool_msgs = [m for m in model.prompts[1] if isinstance(m, ToolMessage)]
    assert "A dog joke." in tool_msgs[0].content and "icanhazdadjoke" in tool_msgs[0].content

    app.invoke({"messages": [HumanMessage("explain it")]}, cfg)
    texts = " ".join(str(m.content) for m in model.prompts[2])
    assert "a joke about dogs" in texts and "A dog joke." in texts


def test_threads_are_separate(monkeypatch):
    app, model, cfg = run(monkeypatch, AIMessage("one"), AIMessage("two"))
    app.invoke({"messages": [HumanMessage("first")]}, cfg)
    app.invoke({"messages": [HumanMessage("second")]}, {"configurable": {"thread_id": "other"}})
    assert "first" not in " ".join(str(m.content) for m in model.prompts[1])


def test_repeated_joke_is_refused(monkeypatch):
    app, _, cfg = run(monkeypatch, call("get_joke", categories=[], type="any", blacklist=[], contains=""),
                      call("get_joke", categories=[], type="any", blacklist=[], contains=""), AIMessage("done"))
    out = app.invoke({"messages": [HumanMessage("joke")]}, cfg)
    results = [m.content for m in out["messages"] if isinstance(m, ToolMessage)]
    assert "The only JokeAPI joke." in results[0]
    assert "already told" in results[1]  # tool errors are results, so the model can adapt


def test_no_match_is_a_tool_result(monkeypatch):
    app, _, cfg = run(monkeypatch, call("search_dad_jokes", term="unicorn"), AIMessage("none"))
    out = app.invoke({"messages": [HumanMessage("unicorn joke")]}, cfg)
    assert '"error"' in [m for m in out["messages"] if isinstance(m, ToolMessage)][0].content


def test_bad_arguments_go_back_to_the_model(monkeypatch):
    app, _, cfg = run(monkeypatch, call("get_joke", categories=["Nonsense"], type="any", blacklist=[], contains=""), AIMessage("sorry"))
    out = app.invoke({"messages": [HumanMessage("joke")]}, cfg)
    assert [m for m in out["messages"] if isinstance(m, ToolMessage)], "validation error should come back as a tool message"
