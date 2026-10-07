# frozen_string_literal: true

require "set"

module AgentLab
  # Stage 3 is a multi-tool agent that remembers the conversation.
  #
  # New compared with stage 2:
  #
  # * Two tools (JokeAPI and icanhazdadjoke). The model has to pick one from
  #   the descriptions alone, which makes tool descriptions part of the prompt.
  # * Memory. The message history persists across user messages, so "another
  #   one", "explain it" and "keep it clean from now on" work. The model has no
  #   memory of its own: "memory" is just us re-sending the transcript.
  # * Context management. The transcript grows every turn but the context
  #   window (num_ctx) doesn't. When it gets close, we drop the oldest turns.
  #   Notice the trade-off: a restriction the user gave early on ("nothing
  #   racist") can be trimmed away. Real systems summarize or pin such facts.
  module Stage3
    SYSTEM_PROMPT = <<~PROMPT.strip
      You are a joke assistant in an ongoing conversation. You get jokes only by calling tools; never write a joke yourself.

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
      request (other tool, broader filters), add one short sentence saying so.
    PROMPT

    # How many times a tool re-fetches when it gets a joke the user has already heard.
    MAX_REPEAT_RETRIES = 3

    class OnlyRepeats < StandardError
      def initialize(msg = "only found jokes already told in this conversation; try different filters or the other tool") = super
    end

    # What the model sees (definition: name, description, JSON schema) paired
    # with what our code does when the model calls it (run). With one tool,
    # stage 2 could hard-code this; with several, a registry like this appears
    # in every agent framework under some name.
    Tool = Struct.new(:definition, :run, keyword_init: true) do
      def name = definition.dig(:function, :name)
    end

    DAD_JOKE_SCHEMA = {
      type: "object",
      properties: { term: { type: "string", description: 'One short search word, or "" for random.' } },
      required: ["term"],
      additionalProperties: false
    }.freeze

    def self.function(name, description, parameters)
      { type: "function", function: { name: name, description: description, parameters: parameters } }
    end

    # "Seen" is memory kept by *our code*, not the model: a set of joke ids
    # already told. Cheap, exact, and it survives history trimming. The block
    # fetches one joke and returns [id, result]; we retry if it was a repeat.
    def self.first_unseen(seen)
      MAX_REPEAT_RETRIES.times do
        key, result = yield
        next if seen.include?(key)

        seen << key
        return result
      end
      raise OnlyRepeats
    end

    def self.joke_api_tool(client, seen)
      Tool.new(
        definition: function(
          "get_joke",
          "Fetch a random joke from JokeAPI. Categories: Programming, Misc, Dark, Pun, Spooky, Christmas. " \
          "Supports content blacklists (nsfw, religious, political, racist, sexist, explicit). " \
          "Best for programming, dark, spooky or Christmas jokes, or when content filters matter.",
          JokeApi.schema
        ),
        run: lambda do |args|
          req = JokeApi::Request.from_h(args).normalize
          first_unseen(seen) do
            j = client.fetch(req)
            ["jokeapi:#{j.id}", { joke: j.text, source: "JokeAPI", category: j.category }]
          end
        end
      )
    end

    def self.dad_joke_tool(client, seen)
      Tool.new(
        definition: function(
          "search_dad_jokes",
          "Fetch a family-friendly dad joke from icanhazdadjoke.com. Searches joke text for one short word " \
          '(e.g. "dog", "ball", "pizza"); multi-word phrases rarely match. Use "" for a random joke.',
          DAD_JOKE_SCHEMA
        ),
        run: lambda do |args|
          term = (args || {})[:term].to_s
          first_unseen(seen) do
            j = term.empty? ? client.random : client.search(term)
            ["dad:#{j.id}", { joke: j.joke, source: "icanhazdadjoke" }]
          end
        end
      )
    end

    class Agent
      attr_accessor :trace, :think
      # Evals hook: called with (tool name, arguments, result) for every tool call.
      attr_accessor :on_tool

      def initialize(llm, jokes, dads, max_steps: 6, max_turns: 10, think: false, trace: Trace::OFF)
        @llm = llm
        @max_steps = max_steps
        @max_turns = max_turns # user messages kept in history
        @think = think
        @trace = trace
        @seen = Set.new
        @tools = [Stage3.joke_api_tool(jokes, @seen), Stage3.dad_joke_tool(dads, @seen)].to_h { |t| [t.name, t] }
        reset
      end

      # Forgets the conversation and the jokes already told.
      def reset
        @history = []         # without the system prompt
        @turns = []           # index in @history where each user turn starts
        @last_in = 0          # prompt tokens of the most recent model call
        @seen.clear
      end

      # Adds the user's message to the conversation and runs the agent loop.
      def respond(text)
        trim
        @turns << @history.size
        @history << { role: "user", content: text }

        (1..@max_steps).each do |step|
          messages = [{ role: "system", content: SYSTEM_PROMPT }] + @history
          resp = @llm.chat(messages: messages, tools: @tools.values.map(&:definition), think: @think)
          @last_in = resp.prompt_eval_count
          msg = resp.message
          @trace.step("step #{step}: model (#{resp.prompt_eval_count} in / #{resp.eval_count} out tokens, " \
                      "#{(resp.total_duration / 1_000_000.0).round}ms, #{@turns.size} turns in memory)", summary(msg))
          @history << msg

          calls = msg[:tool_calls] || []
          return msg[:content] if calls.empty?

          calls.each do |call|
            name = call.dig(:function, :name)
            args = call.dig(:function, :arguments)
            result = run(name, args)
            @trace.step("step #{step}: #{name} result", result)
            @on_tool&.call(name, args, result)
            @history << { role: "tool", tool_name: name, content: result }
          end
        end
        raise Ollama::Error, "gave up after #{@max_steps} steps without a final answer"
      end

      private

      # Runs one tool call. Failures go back to the model as text so it can
      # react (retry, switch tools) instead of the whole turn failing.
      def run(name, args)
        tool = @tools[name]
        return JSON.generate(error: "unknown tool #{name}") unless tool

        JSON.generate(tool.run.call(args))
      rescue JokeApi::Error, DadJoke::Error, Http::Error, OnlyRepeats => e
        JSON.generate(error: e.message)
      end

      # Drops whole turns from the front of the history (never half a turn, or
      # a tool result would lose the call it answers) until we're under
      # max_turns and the last prompt used less than 3/4 of the context window.
      def trim
        budget = @llm.num_ctx.to_i * 3 / 4
        while @turns.size > 1 && (@turns.size >= @max_turns || (budget.positive? && @last_in > budget))
          cut = @turns[1]
          @trace.step("memory", "dropping oldest turn (#{cut} messages): #{@history[0][:content].inspect}")
          @history = @history[cut..]
          @turns = @turns[1..].map { |i| i - cut }
          @last_in = 0 # unknown until the next call; drop one turn at a time
        end
      end

      def summary(msg)
        out = {}
        out[:thinking] = msg[:thinking] unless msg[:thinking].to_s.empty?
        out[:content] = msg[:content] unless msg[:content].to_s.empty?
        calls = (msg[:tool_calls] || []).map { |c| { name: c.dig(:function, :name), arguments: c.dig(:function, :arguments) } }
        out[:tool_calls] = calls unless calls.empty?
        out
      end
    end
  end
end
