# frozen_string_literal: true

require "uri"

module AgentLab
  # A small client for https://icanhazdadjoke.com, a second, always
  # family-friendly joke source with word search.
  module DadJoke
    DEFAULT_BASE_URL = "https://icanhazdadjoke.com"

    class Error < StandardError; end

    # A search found nothing.
    class NoMatch < Error
      def initialize(msg = "dadjoke: no jokes matched the search term") = super
    end

    Joke = Struct.new(:id, :joke, keyword_init: true)

    class Client
      attr_reader :base_url

      def initialize(base_url: DEFAULT_BASE_URL)
        @base_url = base_url
      end

      def random
        get("/")
      end

      # A random joke whose text matches term. The site matches word fragments
      # ("ball" finds "balloon"), so short, common words work best.
      def search(term)
        page = get_json("/search?limit=30&term=#{URI.encode_www_form_component(term)}")
        results = page["results"] || []
        raise NoMatch if results.empty?

        to_joke(results.sample)
      end

      private

      def get(path) = to_joke(get_json(path))

      def to_joke(data) = Joke.new(id: data["id"], joke: data["joke"])

      def get_json(path)
        # The API returns HTML unless asked for JSON, and asks clients to identify themselves.
        res = Http.get(base_url + path, headers: { "Accept" => "application/json",
                                                   "User-Agent" => "agent-lab (https://github.com/AriT93/agent-lab)" })
        raise Error, "dadjoke: HTTP #{res.status}" if res.status >= 300

        JSON.parse(res.body)
      rescue JSON::ParserError => e
        raise Error, "dadjoke: decoding response: #{e.message}"
      end
    end
  end
end
