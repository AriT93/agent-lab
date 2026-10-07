# frozen_string_literal: true

require "test_helper"
require "rack/test"

class WebTest < Minitest::Test
  include Rack::Test::Methods

  def app
    AgentLab::Web.set :llm, llm
    AgentLab::Web.set :jokes, jokes
    AgentLab::Web.set :dads, dads
    AgentLab::Web
  end

  def test_chat_renders_escaped_reply_and_trace_without_ansi
    fake_sources
    script_model(call("search_dad_jokes", { term: "dog" }), say("Why did the <script> cross the road?"))
    post "/chat", text: "a dog joke"
    assert_equal 200, last_response.status
    body = last_response.body
    ["a dog joke", "&lt;script&gt;", "<details", "search_dad_jokes"].each { |want| assert_includes body, want }
    refute_includes body, "<script>"
    refute_includes body, "\e["
  end

  def test_each_browser_has_its_own_conversation_and_reset_forgets
    script_model(*Array.new(4) { say("ok") })
    turns = ->(browser, text) { browser.post("/chat", text: text).tap { |r| r.status }; browser.last_response.body }
    a = Rack::Test::Session.new(Rack::MockSession.new(app))
    b = Rack::Test::Session.new(Rack::MockSession.new(app))
    turns.call(a, "first")
    assert_includes turns.call(b, "second"), "1 turns in memory"
    assert_includes turns.call(a, "again"), "2 turns in memory"
    a.post "/reset"
    assert_equal 204, a.last_response.status
    assert_includes turns.call(a, "fresh"), "1 turns in memory"
  end

  def test_empty_message_is_ignored
    post "/chat", text: "  "
    assert_equal 204, last_response.status
  end

  def test_healthz_is_ok_and_starts_no_conversation
    before = AgentLab::Web::SESSIONS.size
    get "/healthz"
    assert_equal 200, last_response.status
    assert_equal before, AgentLab::Web::SESSIONS.size
  end

  def test_index_sets_cookie
    get "/"
    assert_equal 200, last_response.status
    assert_includes last_response.headers["set-cookie"], "rack.session"
  end
end
