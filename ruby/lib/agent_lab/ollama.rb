# frozen_string_literal: true

require "json"

module AgentLab
  # A minimal client for Ollama's /api/chat endpoint.
  #
  # It is plain Net::HTTP + JSON on purpose: the hashes below *are* the wire
  # format, so reading this file shows you exactly what a "tool call" or a
  # "structured output" request looks like.
  #
  # The client also applies resource limits to every request, because a local
  # model's defaults can be expensive: some models load with a 256K-token
  # context window and the server keeps them in memory for 5 minutes.
  module Ollama
    DEFAULT_MODEL = "qwen3.5:9b-mlx"

    class Error < StandardError; end

    # What comes back from /api/chat. `message` is a hash with :role, :content,
    # and possibly :thinking and :tool_calls ([{function: {name:, arguments:}}]).
    Response = Struct.new(:message, :done_reason, :prompt_eval_count, :eval_count,
                          :total_duration, :load_duration, keyword_init: true)

    class Client
      attr_accessor :model, :num_ctx, :num_predict, :keep_alive
      attr_reader :base_url

      # num_ctx / num_predict / keep_alive: nil means "use Ollama's default",
      # which is usually what eats memory. We default to an 8K context, 2K
      # tokens of output and unloading after 2 minutes idle.
      def initialize(base_url: ENV["OLLAMA_HOST"], model: DEFAULT_MODEL, num_ctx: 8192, num_predict: 2048, keep_alive: "2m")
        base = base_url.to_s.empty? ? "http://localhost:11434" : base_url
        base = "http://#{base}" unless base.include?("://")
        @base_url = base.chomp("/")
        @model = model
        @num_ctx = num_ctx
        @num_predict = num_predict
        @keep_alive = keep_alive
      end

      # One non-streaming request. `format` is a JSON schema (structured
      # output); `think` lets the model reason before answering.
      def chat(messages:, tools: nil, format: nil, think: nil)
        body = { model: model, messages: messages, stream: false, keep_alive: keep_alive,
                 options: { num_ctx: num_ctx, num_predict: num_predict }.compact }
        body[:tools] = tools if tools
        body[:format] = format if format
        body[:think] = think unless think.nil?

        d = post("/api/chat", body)
        Response.new(message: d[:message], done_reason: d[:done_reason], prompt_eval_count: d[:prompt_eval_count].to_i,
                     eval_count: d[:eval_count].to_i, total_duration: d[:total_duration].to_i, load_duration: d[:load_duration].to_i)
      end

      # Asks the server to drop the model from memory now rather than waiting
      # for keep_alive to expire.
      def unload
        post("/api/chat", { model: model, messages: [], keep_alive: "0s" })
      end

      private

      def post(path, body)
        res = Http.post_json(base_url + path, body)
        data = JSON.parse(res.body, symbolize_names: true)
        raise Error, "ollama: #{data[:error] || "HTTP #{res.status}"}" if res.status >= 300

        data
      rescue Http::Error => e
        raise Error, "ollama: #{e.message} (is `ollama serve` running?)"
      rescue JSON::ParserError => e
        raise Error, "ollama: decoding response: #{e.message}"
      end
    end
  end
end
