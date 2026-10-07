# frozen_string_literal: true

module AgentLab
  # Stage 2 is a tool-calling agent with a hand-written loop.
  #
  # Lesson: in stage 1 our code decided what happens after the model answers.
  # Here the model decides. We describe a tool (get_joke), and the model chooses
  # when to call it, with what arguments, and what to do with the result,
  # including retrying with different filters when JokeAPI finds nothing.
  #
  # The whole "agent" is the loop in #respond:
  #
  #   send messages + tool list -> model replies
  #     |- reply has tool calls?  run them, append results, loop
  #     '- plain text?            that's the answer, stop
  #
  # Everything else (frameworks, SDK tool runners) is a convenience over this.
  module Stage2
    SYSTEM_PROMPT = <<~PROMPT.strip
      You are a joke assistant. You get jokes only by calling the get_joke tool; never write a joke yourself.

      Turn the user's request into get_joke filters:
      - categories: only those asked for or clearly implied; [] means any.
      - type: "single" for one-liners, "twopart" for setup/punchline, otherwise "any".
      - blacklist: content the user wants excluded. "Clean" or "family friendly" means every flag.
        If the user says something is fine (e.g. "it can be dirty"), don't blacklist it.
      - contains: a literal substring of the joke text. Multi-word phrases almost never match;
        prefer one short word, or "".

      If get_joke reports no matching joke, call it again with broader filters: first shorten or
      drop "contains", then loosen categories. Never loosen the blacklist. After 3 failed calls,
      tell the user you couldn't find one.

      When you have a joke, reply with its text exactly as returned. If you had to relax a
      filter, add one short sentence saying what you changed.
    PROMPT

    GET_JOKE_TOOL = {
      type: "function",
      function: {
        name: "get_joke",
        description: "Fetch one random joke from JokeAPI matching the filters. Returns the joke, or an error if nothing matches.",
        parameters: JokeApi.schema
      }
    }.freeze

    class Agent
      attr_accessor :trace, :think

      # max_steps caps model calls per user message. Without a cap, a confused
      # model can loop on tool calls forever, and locally each step costs seconds.
      def initialize(llm, jokes, max_steps: 6, think: false, trace: Trace::OFF)
        @llm = llm
        @jokes = jokes
        @max_steps = max_steps
        @think = think
        @trace = trace
      end

      # Runs the agent loop for one user message and returns its reply.
      def respond(text)
        messages = [{ role: "system", content: SYSTEM_PROMPT }, { role: "user", content: text }]

        (1..@max_steps).each do |step|
          resp = @llm.chat(messages: messages, tools: [GET_JOKE_TOOL], think: @think)
          msg = resp.message
          @trace.step("step #{step}: model (#{resp.prompt_eval_count} in / #{resp.eval_count} out tokens, " \
                      "#{(resp.total_duration / 1_000_000.0).round}ms)", summary(msg))

          # The assistant turn, tool calls included, must go back into the
          # history so the model can see what it already asked for.
          messages << msg

          calls = msg[:tool_calls] || []
          if calls.empty?
            raise Ollama::Error, "reply cut off by the output limit (num_predict)" if resp.done_reason == "length"

            return msg[:content]
          end

          calls.each do |call|
            result = run_tool(call)
            @trace.step("step #{step}: #{call.dig(:function, :name)} result", result)
            messages << { role: "tool", tool_name: call.dig(:function, :name), content: result }
          end
        end
        raise Ollama::Error, "gave up after #{@max_steps} steps without a final answer"
      end

      private

      # Executes one tool call and returns what the model will see. Errors are
      # returned *to the model* as text rather than raised: telling the model
      # "no matching joke" is how it learns to retry with other filters.
      def run_tool(call)
        name = call.dig(:function, :name)
        return JSON.generate(error: "unknown tool #{name}") unless name == "get_joke"

        req = JokeApi::Request.from_h(call.dig(:function, :arguments)).normalize
        @trace.step("GET", req.url(@jokes.base_url))
        j = @jokes.fetch(req)
        JSON.generate(joke: j.text, category: j.category, id: j.id)
      rescue JokeApi::Error, Http::Error => e
        JSON.generate(error: e.message)
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
