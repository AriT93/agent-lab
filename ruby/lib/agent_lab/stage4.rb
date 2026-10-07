# frozen_string_literal: true

require "json"

module AgentLab
  # Stage 4 exposes the joke tools as an MCP (Model Context Protocol) server
  # over stdio, so any MCP client (Claude Code, Claude Desktop, ...) can use them.
  #
  # Lesson: in stages 2-3 the tool list lived inside our agent, wired to one
  # model API. MCP moves the tools behind a standard protocol: the *client*
  # owns the model and the loop, the *server* only describes and runs tools.
  # Our agent loop isn't needed here at all.
  #
  # The protocol is small enough to write by hand. It is JSON-RPC 2.0, one JSON
  # message per line on stdin/stdout:
  #
  #   client -> initialize                  server -> its name, version, capabilities
  #   client -> notifications/initialized   (no reply: notifications have no id)
  #   client -> tools/list                  server -> [{name, description, inputSchema}]
  #   client -> tools/call                  server -> {content: [{type:"text", text}], isError}
  #
  # stdout is the protocol channel, so all logging (the --trace output) goes to
  # stderr. A tool failure is a normal result with isError=true, not a JSON-RPC
  # error, so the calling model sees it and can retry, same as in stage 2/3.
  module Stage4
    # The MCP revision this server speaks. Clients that ask for another
    # version are told this one and decide whether to continue.
    PROTOCOL_VERSION = "2025-06-18"

    # Standard JSON-RPC error codes.
    CODE_PARSE = -32_700
    CODE_METHOD_NOT_FOUND = -32_601
    CODE_INVALID_PARAMS = -32_602

    # What the server lists (name, description, input_schema) plus what it does
    # on tools/call (run). Mirrors stage 3's Tool, with MCP's field names.
    Tool = Struct.new(:name, :description, :input_schema, :run, keyword_init: true) do
      def listing = { name: name, description: description, inputSchema: input_schema }
    end

    # Unlike stage 3 there is no "seen jokes" memory: an MCP server doesn't
    # know which conversation a call belongs to, so de-duplicating is the
    # client's job.
    def self.server(jokes, dads, trace: Trace::OFF)
      Server.new(name: "agent-lab-jokes", version: "0.1.0", tools: [joke_api_tool(jokes), dad_joke_tool(dads)], trace: trace)
    end

    def self.joke_api_tool(client)
      Tool.new(
        name: "get_joke",
        description: "Fetch a random joke from JokeAPI. Categories: Programming, Misc, Dark, Pun, Spooky, Christmas. " \
                     "Supports content blacklists (nsfw, religious, political, racist, sexist, explicit). " \
                     "Best for programming, dark, spooky or Christmas jokes, or when content filters matter.",
        input_schema: JokeApi.schema,
        run: ->(args) { client.fetch(JokeApi::Request.from_h(args).normalize).text }
      )
    end

    def self.dad_joke_tool(client)
      Tool.new(
        name: "search_dad_jokes",
        description: "Fetch a family-friendly dad joke from icanhazdadjoke.com. Searches joke text for one short word " \
                     '(e.g. "dog", "ball", "pizza"); multi-word phrases rarely match. Use "" for a random joke.',
        input_schema: Stage3::DAD_JOKE_SCHEMA,
        run: lambda do |args|
          term = (args || {})[:term].to_s
          (term.empty? ? client.random : client.search(term)).joke
        end
      )
    end

    class Server
      attr_reader :tools

      def initialize(name:, version:, tools:, trace: Trace::OFF)
        @name = name
        @version = version
        @tools = tools
        @trace = trace
      end

      # Reads requests from input and writes responses to output until input closes.
      def serve(input = $stdin, output = $stdout)
        output.sync = true
        input.each_line do |line|
          line = line.strip
          next if line.empty?

          @trace.step("← client", line)
          response = handle(line)
          next unless response # notifications get no reply

          json = JSON.generate(response)
          @trace.step("→ client", json)
          output.puts(json)
        end
      end

      # Returns a response hash, or nil for notifications.
      def handle(line)
        request = JSON.parse(line, symbolize_names: true)
        return nil unless request.key?(:id)

        response = { jsonrpc: "2.0", id: request[:id] }
        case request[:method]
        when "initialize"
          response[:result] = { protocolVersion: PROTOCOL_VERSION, capabilities: { tools: {} },
                                serverInfo: { name: @name, version: @version } }
        when "ping" then response[:result] = {}
        when "tools/list" then response[:result] = { tools: @tools.map(&:listing) }
        when "tools/call" then response.merge!(call(request[:params]))
        else response[:error] = { code: CODE_METHOD_NOT_FOUND, message: "method not found: #{request[:method]}" }
        end
        response
      rescue JSON::ParserError => e
        { jsonrpc: "2.0", id: nil, error: { code: CODE_PARSE, message: e.message } }
      end

      private

      def call(params)
        tool = @tools.find { |t| t.name == params&.dig(:name) }
        return { error: { code: CODE_INVALID_PARAMS, message: "unknown tool #{params&.dig(:name).inspect}" } } unless tool

        text = tool.run.call(params[:arguments] || {})
        { result: { content: [{ type: "text", text: text }], isError: false } }
      rescue JokeApi::Error, DadJoke::Error, Http::Error => e
        { result: { content: [{ type: "text", text: e.message }], isError: true } }
      end
    end
  end
end
