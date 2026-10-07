# frozen_string_literal: true

require "uri"

module AgentLab
  # A small client for https://v2.jokeapi.dev.
  #
  # It deliberately knows nothing about natural language: callers hand it a
  # typed Request. Turning "tell me a clean programming joke" into a Request is
  # the job of an interpreter (the stages), which is the part we swap out as we
  # add more LLM involvement.
  module JokeApi
    CATEGORIES = %w[Programming Misc Dark Pun Spooky Christmas].freeze
    # Flags mark content a joke contains; flags in Request#blacklist are excluded.
    FLAGS = %w[nsfw religious political racist sexist explicit].freeze
    DEFAULT_BASE_URL = "https://v2.jokeapi.dev/joke"

    # JokeAPI said no ("error": true), e.g. code 106 "No matching joke found"
    # when the filters are too narrow.
    class Error < StandardError; end

    # Which joke to fetch. The default means "any joke".
    class Request < Struct.new(:categories, :type, :blacklist, :contains, keyword_init: true)
      def initialize(categories: [], type: "", blacklist: [], contains: "")
        super
      end

      # Builds a Request from decoded JSON (string or symbol keys); nils become defaults.
      def self.from_h(hash)
        h = (hash || {}).transform_keys(&:to_sym)
        new(categories: Array(h[:categories]), type: h[:type].to_s,
            blacklist: Array(h[:blacklist]), contains: h[:contains].to_s)
      end

      def to_h
        { categories: categories, type: type, blacklist: blacklist, contains: contains }
      end

      # Drops unknown and duplicate values and maps "any" to "". Models,
      # especially small local ones, produce these even under a schema.
      def normalize
        Request.new(
          categories: canonical(categories, CATEGORIES),
          type: %w[single twopart].include?(type) ? type : "",
          blacklist: canonical(blacklist, FLAGS),
          contains: contains.to_s.strip
        )
      end

      def url(base)
        path = categories.empty? ? "Any" : categories.join(",")
        query = {}
        query["type"] = type if %w[single twopart].include?(type)
        query["blacklistFlags"] = blacklist.join(",") unless blacklist.empty?
        query["contains"] = contains unless contains.empty?
        url = "#{base.chomp("/")}/#{path}"
        query.empty? ? url : "#{url}?#{URI.encode_www_form(query)}"
      end

      private

      def canonical(values, known)
        Array(values).filter_map { |v| known.find { |k| k.casecmp?(v.to_s) } }.uniq
      end
    end

    # The subset of JokeAPI's response we use.
    Joke = Struct.new(:id, :category, :type, :joke, :setup, :delivery, keyword_init: true) do
      def text
        type == "twopart" ? "#{setup}\n\n#{delivery}" : joke
      end
    end

    # The JSON schema for a Request. It is both the structured-output format
    # (stage 1) and the tool's parameter schema (stages 2+).
    def self.schema
      {
        type: "object",
        properties: {
          categories: { type: "array", items: { type: "string", enum: CATEGORIES },
                        description: "Categories to draw from; empty means any." },
          type: { type: "string", enum: %w[any single twopart],
                  description: '"single" is a one-liner, "twopart" is setup + punchline.' },
          blacklist: { type: "array", items: { type: "string", enum: FLAGS },
                       description: "Content to exclude." },
          contains: { type: "string",
                      description: 'Literal substring the joke text must contain. Rarely matches multi-word phrases; use "" for none.' }
        },
        required: %w[categories type blacklist contains],
        additionalProperties: false
      }
    end

    class Client
      attr_reader :base_url

      def initialize(base_url: DEFAULT_BASE_URL)
        @base_url = base_url
      end

      # Raises JokeApi::Error (including "no matching joke") or Http::Error.
      def fetch(request)
        res = Http.get(request.url(base_url))
        # JokeAPI reports "no match" and bad filters as JSON with "error": true,
        # sometimes alongside a non-2xx status, so check the body first.
        data = parse(res)
        raise Error, error_message(data) if data["error"]
        raise Error, "jokeapi: HTTP #{res.status}" unless res.status < 300

        Joke.new(id: data["id"], category: data["category"], type: data["type"],
                 joke: data["joke"], setup: data["setup"], delivery: data["delivery"])
      end

      private

      def parse(res)
        JSON.parse(res.body)
      rescue JSON::ParserError
        raise Error, res.status >= 300 ? "jokeapi: HTTP #{res.status}" : "jokeapi: could not decode the response"
      end

      def error_message(data)
        msg = "jokeapi: #{data["message"]} (code #{data["code"]})"
        causes = Array(data["causedBy"])
        causes.empty? ? msg : "#{msg}: #{causes.join("; ")}"
      end
    end
  end
end
