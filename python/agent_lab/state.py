import operator
from typing import Annotated

from langgraph.graph import MessagesState


class State(MessagesState):
    """The graph's state: the conversation (messages, from MessagesState) plus
    the ids of jokes already told. `operator.add` is the reducer: a node returns
    only the new ids and LangGraph appends them."""

    seen: Annotated[list[str], operator.add]
