# frozen_string_literal: true

module AgentLab
  # Stage 1 interprets requests with one LLM call that is constrained to return
  # JSON matching a schema ("structured output").
  #
  # Lesson: the LLM is a *parser* here, not an agent. Our code still decides
  # everything else; the model only fills in a typed form. Compare this with
  # the original project, which asked for "category=x&type=y" as free text and
  # then fed that text to a keyword matcher, so the blacklist was silently
  # lost. With a schema, the model can't return anything we can't decode.
  #
  # (The Go version also has a Claude API backend. That needs a Console API
  # key, so it isn't ported.)
  module Stage1
    SYSTEM_PROMPT = <<~PROMPT.strip
      You translate a user's joke request into filters for JokeAPI.

      - categories: only the categories the user asked for or clearly implied; empty means any.
      - type: "single" for one-liners, "twopart" for setup/punchline, otherwise "any".
      - blacklist: content the user wants excluded. "Clean", "safe" or "family friendly"
        means blacklist every flag. If the user says something is fine (e.g. "it can be
        dirty"), do not blacklist it.
      - contains: JokeAPI matches this as a literal substring of the joke text, so it
        rarely matches multi-word topics. Use a single short word only when the user
        asks for a specific subject; otherwise use "".
    PROMPT

    class Interpreter
      def initialize(client, trace: Trace::OFF)
        @client = client
        @trace = trace
      end

      def interpret(text)
        @trace.step("stage1: request to #{@client.model}", { system: SYSTEM_PROMPT, user: text })
        resp = @client.chat(
          messages: [{ role: "system", content: SYSTEM_PROMPT }, { role: "user", content: text }],
          # Ollama turns the schema into a grammar, so the model can only emit
          # tokens that keep the JSON valid against it.
          format: JokeApi.schema,
          think: false # a form-filling task doesn't need reasoning; thinking costs seconds locally
        )
        @trace.step("stage1: usage", usage(resp))
        @trace.step("stage1: model output", resp.message[:content])

        raise Ollama::Error, "model hit the output limit (num_predict) before finishing" if resp.done_reason == "length"

        JokeApi::Request.from_h(JSON.parse(resp.message[:content])).normalize
      rescue JSON::ParserError => e
        raise Ollama::Error, "decoding model output: #{e.message}"
      end

      private

      def usage(resp)
        { done_reason: resp.done_reason, input_tokens: resp.prompt_eval_count, output_tokens: resp.eval_count,
          total: ms(resp.total_duration), load: ms(resp.load_duration) }
      end

      def ms(nanoseconds) = "#{(nanoseconds / 1_000_000.0).round}ms"
    end
  end
end
