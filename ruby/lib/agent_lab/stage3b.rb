# frozen_string_literal: true

require "langchain"

# The assistant warns on every call that Ollama ignores tool_choice and parallel_tool_calls.
Langchain.logger.level = Logger::ERROR

module AgentLab
  # Stage 3b is stage 3 again, this time in langchainrb, to see what a
  # framework hides (and what it doesn't).
  #
  # What the framework does for you:
  #
  # * The agent loop. Langchain::Assistant#run! calls the model, runs its tool
  #   calls and loops until a plain-text reply: stage 2's for-loop, gone.
  # * Memory. The assistant keeps the message list, so "another one" works.
  # * Tool schemas. `define_function` builds the JSON schema from a block, and
  #   the model's arguments arrive as keyword arguments to a Ruby method.
  #
  # What it takes away, and what you notice when you run it with --trace:
  #
  # * Tool names. The model sees "joke_api__get_joke" (tool name + method), so
  #   the system prompt has to use those names, not the ones from stage 3.
  # * No step cap. The assistant loops until the model stops calling tools. We
  #   add the cap back through the tool_execution_callback.
  # * No trimming. Messages only grow; nothing drops old turns before num_ctx
  #   fills up, which stage 3 handled.
  # * Quiet failures. If a tool raises, the run ends in state :failed with no
  #   reply. Our tools therefore rescue everything and return errors as text.
  # * Ollama limits. keep_alive and think are not parameters the client knows,
  #   so we register them; num_ctx / num_predict go through `options`.
  # * No per-call token counts or timing in the message stream; the trace
  #   reports the assistant's running totals instead.
  module Stage3b
    SYSTEM_PROMPT = <<~PROMPT.strip
      You are a joke assistant in an ongoing conversation. You get jokes only by calling tools; never write a joke yourself.

      Choosing a tool:
      - dad_jokes__search: clean dad jokes, searchable by one short word. Good for everyday topics
        (animals, food, sports) and when the user wants something family friendly.
      - joke_api__get_joke: JokeAPI. Good for programming, dark, pun, spooky or Christmas jokes, and when the
        user sets content limits. Turn "clean"/"family friendly" into a full blacklist.

      If a tool finds nothing, try a shorter or related word, the other tool, or broader filters.
      Never loosen content limits the user asked for, including ones from earlier in the
      conversation. After 3 failed tool calls in a row, say you couldn't find one.

      Use the conversation: "another one" means a new joke like the last request; questions about
      a joke you told ("explain it", "why is that funny") need no tool.

      When you tell a joke, give its text exactly as the tool returned it. If you changed the
      request (other tool, broader filters), add one short sentence saying so.
    PROMPT

    # Stage 3's "seen" memory and error handling, shared by both tools. Errors
    # come back as {"error": ...} text so the model can retry.
    module Told
      def told(seen, key)
        return JSON.generate(error: Stage3::OnlyRepeats.new.message) if seen.include?(key)

        seen << key
        nil
      end

      def attempt
        Stage3::MAX_REPEAT_RETRIES.times do
          result = yield
          return JSON.generate(result) if result
        end
        JSON.generate(error: Stage3::OnlyRepeats.new.message)
      rescue JokeApi::Error, DadJoke::Error, Http::Error => e
        JSON.generate(error: e.message)
      end
    end

    class JokeApiTool
      extend Langchain::ToolDefinition
      include Told

      def self.tool_name = "joke_api"

      define_function :get_joke, description: "Fetch a random joke from JokeAPI. Categories: Programming, Misc, Dark, Pun, Spooky, Christmas. " \
                                              "Supports content blacklists (nsfw, religious, political, racist, sexist, explicit). " \
                                              "Best for programming, dark, spooky or Christmas jokes, or when content filters matter." do
        property :categories, type: "array", description: "Categories to draw from; empty means any.", required: true do
          item type: "string", enum: JokeApi::CATEGORIES
        end
        property :type, type: "string", description: '"single" is a one-liner, "twopart" is setup + punchline.',
                        enum: %w[any single twopart], required: true
        property :blacklist, type: "array", description: "Content to exclude.", required: true do
          item type: "string", enum: JokeApi::FLAGS
        end
        property :contains, type: "string", required: true,
                            description: 'Literal substring the joke text must contain. Rarely matches multi-word phrases; use "" for none.'
      end

      def initialize(client, seen)
        @client = client
        @seen = seen
      end

      # The model's arguments arrive as keywords; `**` swallows any it invents.
      def get_joke(**args)
        req = JokeApi::Request.from_h(args).normalize
        attempt do
          j = @client.fetch(req)
          { joke: j.text, source: "JokeAPI", category: j.category } unless told(@seen, "jokeapi:#{j.id}")
        end
      end
    end

    class DadJokesTool
      extend Langchain::ToolDefinition
      include Told

      def self.tool_name = "dad_jokes"

      define_function :search, description: "Fetch a family-friendly dad joke from icanhazdadjoke.com. Searches joke text for one short word " \
                                            '(e.g. "dog", "ball", "pizza"); multi-word phrases rarely match. Use "" for a random joke.' do
        property :term, type: "string", description: 'One short search word, or "" for random.', required: true
      end

      def initialize(client, seen)
        @client = client
        @seen = seen
      end

      def search(term: "", **)
        attempt do
          j = term.to_s.empty? ? @client.random : @client.search(term.to_s)
          { joke: j.joke, source: "icanhazdadjoke" } unless told(@seen, "dad:#{j.id}")
        end
      end
    end

    class Agent
      attr_accessor :trace

      def initialize(llm_client, jokes, dads, max_steps: 6, think: false, trace: Trace::OFF)
        @trace = trace
        @max_steps = max_steps
        @seen = Set.new
        @jokes = jokes
        @dads = dads
        @think = think
        @ollama = llm_client
        reset
      end

      # Forgets the conversation and the jokes already told.
      def reset
        @seen.clear
        @steps = 0
        @assistant = build_assistant
      end

      def respond(text)
        @steps = 0
        @assistant.add_message_and_run!(content: text)
        reply = @assistant.messages.last
        if @assistant.state == :failed || reply.role != "assistant" || !reply.tool_calls.to_a.empty?
          # The framework swallowed the error (see "Quiet failures" above); the
          # trace has the details.
          raise Ollama::Error, "assistant stopped without a reply (state: #{@assistant.state}); gave up after #{@max_steps} tool calls?"
        end

        reply.content
      end

      private

      def build_assistant
        llm = Langchain::LLM::Ollama.new(
          url: @ollama.base_url,
          default_options: { chat_model: @ollama.model, temperature: nil,
                             options: { num_ctx: @ollama.num_ctx, num_predict: @ollama.num_predict } }
        )
        # Not in the client's parameter list, so register them (see the header).
        llm.chat_parameters.update(keep_alive: { default: @ollama.keep_alive }, think: { default: @think })

        assistant = Langchain::Assistant.new(
          llm: llm,
          instructions: SYSTEM_PROMPT,
          tools: [JokeApiTool.new(@jokes, @seen), DadJokesTool.new(@dads, @seen)],
          add_message_callback: method(:trace_message),
          tool_execution_callback: method(:before_tool)
        )
        @trace.step("stage3b: system prompt", SYSTEM_PROMPT)
        assistant
      end

      # Raising here ends the run (the assistant rescues it and goes to
      # :failed), which is the only way to stop a looping model.
      def before_tool(_id, tool, method, args)
        @steps += 1
        @trace.step("tool call #{@steps}", { tool: "#{tool}__#{method}", arguments: args })
        raise "too many tool calls" if @steps > @max_steps
      end

      def trace_message(message)
        body = { role: message.role }
        body[:content] = message.content unless message.content.to_s.empty?
        calls = message.tool_calls.to_a.map { |c| { name: c.dig("function", "name"), arguments: c.dig("function", "arguments") } }
        body[:tool_calls] = calls unless calls.empty?
        totals = @assistant && "#{@assistant.total_prompt_tokens} in / #{@assistant.total_completion_tokens} out tokens so far"
        @trace.step("message#{" (#{totals})" if totals}", body)
      end
    end
  end
end
