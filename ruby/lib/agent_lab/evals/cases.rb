# frozen_string_literal: true

module AgentLab
  module Evals
    # The tables of cases. Cases marked "known weakness" are here on purpose:
    # they should fail until the prompt or schema is improved, and then they
    # guard the fix.
    module Cases
      C = Checks
      JOKE_TOOL = "get_joke"
      DAD_TOOL = "search_dad_jokes"

      def self.req(name, prompt, *checks) = RequestCase.new(name, prompt, checks)

      def self.chat(name, *turns) = ConversationCase.new(name, turns)

      def self.turn(say, *checks) = Turn.new(say, checks)

      # For stages 0 and 1.
      REQUESTS = [
        req("any-joke", "tell me a joke", C.no_blacklist, C.no_contains, C.has_type("")),
        req("category", "a programming joke", C.has_categories("Programming")),
        req("two-categories", "something dark or punny", C.has_categories("Dark", "Pun")),
        req("no-topic-filter", "a spooky joke", C.has_categories("Spooky"), C.no_contains),
        req("one-liner", "give me a one-liner", C.has_type("single")),
        req("two-part", "a two-part joke please", C.has_type("twopart")),
        req("clean", "keep it clean", C.blacklists_all),
        req("family-friendly", "something family friendly", C.blacklists_all),
        req("clean-and-category", "a clean christmas joke", C.has_categories("Christmas"), C.blacklists_all),
        req("two-flags", "nothing about politics or religion", C.blacklists("political", "religious")),
        req("allowed-flag", "a joke about computers, it can be dirty but not racist",
            C.blacklists("racist"), C.allows("nsfw", "explicit")),
        req("topic-word", "a joke about dogs", C.contains_word("dog", "dogs")),
        req("multiword-topic", "a joke about the chicago cubs", C.contains_word("cubs", "chicago", "cub"))
      ].freeze

      # For the agents (stage 3).
      CONVERSATIONS = [
        chat("picks-jokeapi-for-code",
             turn("a programming joke", C.uses_tool(JOKE_TOOL), C.reply_contains_tool_joke)),
        chat("clean-expands-in-agent",
             turn("a clean programming joke", C.every_call(JOKE_TOOL, C.blacklists_all))),
        chat("another-one-uses-tool",
             turn("a joke about dogs", C.uses_any_tool(JOKE_TOOL, DAD_TOOL)),
             turn("another one", C.uses_any_tool(JOKE_TOOL, DAD_TOOL), C.reply_contains_tool_joke)),
        chat("explain-needs-no-tool",
             turn("a joke about dogs", C.uses_any_tool(JOKE_TOOL, DAD_TOOL)),
             turn("explain that joke", C.no_tools)),
        # Known weakness: after an "explain it" turn the agent explains later jokes unasked.
        chat("no-unasked-explanations",
             turn("a joke about dogs", C.uses_any_tool(JOKE_TOOL, DAD_TOOL)),
             turn("explain that joke", C.no_tools),
             turn("another one", C.uses_any_tool(JOKE_TOOL, DAD_TOOL), C.short_reply(150))),
        chat("restriction-persists",
             turn("from now on nothing political. a programming joke", C.every_call(JOKE_TOOL, C.blacklists("political"))),
             turn("another one", C.uses_any_tool(JOKE_TOOL, DAD_TOOL), C.every_call(JOKE_TOOL, C.blacklists("political"))))
      ].freeze
    end
  end
end
