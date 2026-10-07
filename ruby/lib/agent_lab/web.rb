# frozen_string_literal: true

require "securerandom"
require "sinatra/base"
require "stringio"

module AgentLab
  # Stage 7: a browser chat UI for the stage 3 agent, built with Sinatra.
  #
  # The lesson is where an agent lives in a web app. The agent is a plain Ruby
  # object that holds one conversation, so the server keeps one per browser
  # (found by a session cookie) and calls it in-process from the route. There
  # is no joke API and no MCP hop: MCP is for other programs (Claude Code) to
  # borrow the tools, and this app already has them. The page is
  # server-rendered; the only JavaScript posts the form and appends the HTML
  # fragment that comes back.
  #
  # Each session's trace output is captured into a buffer and shown under the
  # reply, so you can watch prompts, tool calls and token counts in the browser.
  class Web < Sinatra::Base
    # Every session holds a conversation, so bound memory: past this, the
    # least recently used one is dropped (that browser just starts a fresh chat).
    MAX_SESSIONS = 20

    Conversation = Struct.new(:agent, :trace_buffer, :lock, :used_at)

    set :root, File.expand_path("../..", __dir__)
    set :views, File.join(root, "views")
    set :llm, nil   # AgentLab::Ollama::Client
    set :jokes, nil # AgentLab::JokeApi::Client
    set :dads, nil  # AgentLab::DadJoke::Client
    enable :sessions
    set :session_secret, ENV.fetch("SESSION_SECRET") { SecureRandom.hex(64) }
    set :server, :puma

    helpers do
      def h(text) = Rack::Utils.escape_html(text.to_s)
    end

    get "/" do
      conversation # sets the cookie
      erb :index
    end

    post "/chat" do
      text = params[:text].to_s.strip
      halt 204 if text.empty?

      convo = conversation
      convo.lock.synchronize do
        @user = text
        begin
          @reply = convo.agent.respond(text)
        rescue Ollama::Error, Http::Error => e
          @error = e.message
        end
        @trace = clear(convo.trace_buffer)
      end
      erb :turn, layout: false
    end

    post "/reset" do
      convo = conversation
      convo.lock.synchronize do
        convo.agent.reset
        clear(convo.trace_buffer)
      end
      204
    end

    SESSIONS = {}
    SESSIONS_LOCK = Mutex.new

    # The caller's conversation, or a new one.
    def conversation
      SESSIONS_LOCK.synchronize do
        convo = SESSIONS[session[:id]]
        unless convo
          id = session[:id] = SecureRandom.hex(16)
          convo = SESSIONS[id] = new_conversation
          SESSIONS.delete(SESSIONS.min_by { |_, c| c.used_at }.first) if SESSIONS.size > MAX_SESSIONS
        end
        convo.used_at = Time.now
        convo
      end
    end

    def new_conversation
      buffer = StringIO.new
      agent = Stage3::Agent.new(settings.llm, settings.jokes, settings.dads, trace: Trace.new(buffer, color: false))
      Conversation.new(agent, buffer, Mutex.new, Time.now)
    end

    # Returns what the buffer held and empties it.
    def clear(buffer)
      text = buffer.string.dup
      buffer.truncate(0)
      buffer.rewind
      text
    end
  end
end
