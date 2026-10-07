# frozen_string_literal: true

module AgentLab
  # Stage 8 is the LangGraph agent in python/. This file is only a client for it: it drives
  # the LangGraph server (`langgraph dev`) over HTTP, one thread per conversation, so the
  # eval table from stage 5 can score the Python agent without a Python port of the evals.
  #
  # The server saves each thread's state. A run returns the whole message list, so the new
  # messages for one turn are the ones after the count we saw last time. Tool calls and their
  # results are read out of those messages.
  module Stage8
    class Agent
      # Evals hook: called with (tool name, arguments, result) for every tool call.
      attr_accessor :on_tool

      def initialize(url: ENV.fetch("LANGGRAPH_URL", "http://127.0.0.1:2024"), assistant: "agent")
        @url = url.chomp("/")
        @assistant = assistant
        reset
      end

      # A new thread is a new conversation.
      def reset
        @thread = post("/threads", {})[:thread_id]
        @seen_messages = 0
      end

      def respond(text)
        state = post("/threads/#{@thread}/runs/wait",
                     { assistant_id: @assistant, input: { messages: [{ role: "human", content: text }] } })
        raise Ollama::Error, "langgraph: #{state[:__error__][:message] || state[:__error__]}" if state[:__error__]

        messages = state[:messages]
        fresh = messages[@seen_messages..]
        @seen_messages = messages.size
        record(fresh)
        reply = fresh.reverse.find { |m| m[:type] == "ai" && Array(m[:tool_calls]).empty? }
        raise Ollama::Error, "langgraph: the run ended without a reply" unless reply

        reply[:content].to_s
      end

      private

      def record(messages)
        results = messages.select { |m| m[:type] == "tool" }.to_h { |m| [m[:tool_call_id], m[:content]] }
        messages.each do |m|
          Array(m[:tool_calls]).each do |call|
            @on_tool&.call(call[:name], call[:args], results[call[:id]].to_s)
          end
        end
      end

      def post(path, body)
        res = Http.post_json(@url + path, body)
        raise Ollama::Error, "langgraph: HTTP #{res.status}: #{res.body[0, 200]}" if res.status >= 300

        JSON.parse(res.body, symbolize_names: true)
      rescue Http::Error => e
        raise Ollama::Error, "langgraph: #{e.message} (is `langgraph dev` running?)"
      end
    end
  end
end
