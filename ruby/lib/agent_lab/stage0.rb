# frozen_string_literal: true

module AgentLab
  # Stage 0 interprets requests with plain keyword matching. No LLM.
  #
  # This is the baseline from the original "simple" project. It is fast, free
  # and predictable, and it is also brittle: "keep it clean" or "no politics"
  # slip straight past it. Every later stage is trying to beat this one.
  module Stage0
    class Interpreter
      # Turns what the user typed into a JokeApi::Request.
      def interpret(text)
        text = text.downcase
        r = JokeApi::Request.new

        if text.include?("twopart") || text.include?("two-part")
          r.type = "twopart"
        elsif text.include?("single") || text.include?("one-liner")
          r.type = "single"
        end

        r.categories = JokeApi::CATEGORIES.select { |c| text.include?(c.downcase) }
        r.blacklist = JokeApi::FLAGS.select { |f| text.include?("no #{f}") || text.include?("not #{f}") }
        r.blacklist << "nsfw" if text.include?("clean") && !r.blacklist.include?("nsfw")
        r
      end
    end
  end
end
