# frozen_string_literal: true

module AgentLab
  module Evals
    # Each method returns a lambda: nil if the value is acceptable, otherwise why not.
    # Request checks take a JokeApi::Request; outcome checks take an Evals::Outcome.
    module Checks
      module_function

      # ---- on a JokeApi::Request (stages 0-1) ----

      # Passes when the request names exactly these categories.
      def has_categories(*want)
        lambda do |r|
          "categories = #{r.categories}, want #{want}" unless r.categories.sort == want.sort
        end
      end

      def has_type(want)
        ->(r) { "type = #{r.type.inspect}, want #{want.inspect}" unless r.type == want }
      end

      # Passes when every flag in want is blacklisted.
      def blacklists(*want)
        lambda do |r|
          missing = want - r.blacklist
          "blacklist = #{r.blacklist}, missing #{missing}" unless missing.empty?
        end
      end

      # Passes when every JokeAPI flag is blacklisted ("clean").
      def blacklists_all = blacklists(*JokeApi::FLAGS)

      # Passes when none of these flags is blacklisted.
      def allows(*flags)
        lambda do |r|
          bad = flags & r.blacklist
          "blacklist = #{r.blacklist}, but the user allowed #{bad.first.inspect}" unless bad.empty?
        end
      end

      def no_blacklist
        ->(r) { "blacklist = #{r.blacklist}, want none" unless r.blacklist.empty? }
      end

      # Passes when the request filters on exactly one word, any of want.
      def contains_word(*want)
        lambda do |r|
          got = r.contains.strip.downcase
          "contains = #{r.contains.inspect}, want one of #{want}" unless want.any? { |w| w.downcase == got }
        end
      end

      def no_contains
        ->(r) { "contains = #{r.contains.inspect}, want none" unless r.contains.empty? }
      end

      # ---- on an Evals::Outcome (stage 3) ----

      # Passes when the turn called the named tool at least once.
      def uses_tool(name) = uses_any_tool(name)

      # Passes when the turn called at least one of the named tools.
      def uses_any_tool(*names)
        lambda do |o|
          "expected one of #{names}, calls: #{tool_names(o.calls)}" unless o.calls.any? { |c| names.include?(c.tool) }
        end
      end

      # Passes when the turn answered without calling any tool.
      def no_tools
        lambda do |o|
          "called tools (#{tool_names(o.calls)}), expected an answer from memory" unless o.calls.empty?
        end
      end

      # Passes when check holds for the arguments of each call to tool. Use it
      # for restrictions that must never be dropped, even on retries.
      def every_call(tool, check)
        lambda do |o|
          o.calls.select { |c| c.tool == tool }.each do |c|
            why = check.call(JokeApi::Request.from_h(c.args).normalize)
            return "#{tool}(#{c.args.to_json}): #{why}" if why
          end
          nil
        end
      end

      # Passes when the reply includes the joke text from the last successful
      # tool result (the agent must repeat jokes verbatim).
      def reply_contains_tool_joke
        lambda do |o|
          joke = last_joke(o)
          if joke.empty? then "no tool returned a joke"
          elsif !o.reply.to_s.include?(joke) then "reply does not contain the tool's joke #{joke.inspect}"
          end
        end
      end

      # Passes when the reply is at most n characters longer than the joke it
      # tells. An agent that explains a joke nobody asked about fails this.
      def short_reply(n)
        lambda do |o|
          joke = last_joke(o)
          len = o.reply.to_s.length
          "reply is #{len} chars for a #{joke.length}-char joke: explained unasked?" if len > joke.length + n
        end
      end

      def tool_names(calls) = calls.empty? ? "none" : calls.map(&:tool).join(", ")

      # The "joke" from the last tool result like {"joke": "...", ...}.
      def last_joke(outcome)
        outcome.calls.reverse_each do |c|
          joke = JSON.parse(c.result)["joke"]
          return joke if joke.is_a?(String) && !joke.empty?
        rescue JSON::ParserError
          next
        end
        ""
      end
    end
  end
end
