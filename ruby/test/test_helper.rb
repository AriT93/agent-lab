# frozen_string_literal: true

ENV["APP_ENV"] = "test"
$LOAD_PATH.unshift File.expand_path("../lib", __dir__)

require "minitest/autorun"
require "webmock/minitest"
require "agent_lab"

WebMock.disable_net_connect!

module Fakes
  OLLAMA = "http://localhost:11434/api/chat"

  # Scripts the model: each /api/chat call gets the next canned assistant
  # message. Returns the list of request bodies the model received.
  def script_model(*replies)
    requests = []
    stub_request(:post, OLLAMA).to_return do |req|
      requests << JSON.parse(req.body, symbolize_names: true)
      raise "model called #{requests.size} times, only #{replies.size} replies scripted" if requests.size > replies.size

      { body: JSON.generate(message: replies[requests.size - 1], done_reason: "stop", prompt_eval_count: 100, eval_count: 10) }
    end
    requests
  end

  def say(text) = { role: "assistant", content: text }

  def call(name, args) = { role: "assistant", content: "", tool_calls: [{ function: { name: name, arguments: args } }] }

  # JokeAPI (always joke id 1) and icanhazdadjoke (one dog joke, one random).
  def fake_sources
    stub_request(:get, %r{\Ahttps://v2\.jokeapi\.dev/joke/}).to_return(
      body: '{"error":false,"type":"single","joke":"The only JokeAPI joke.","category":"Misc","id":1}'
    )
    # WebMock tries the most recently declared stub first, so the specific one goes last.
    stub_request(:get, %r{\Ahttps://icanhazdadjoke\.com/search}).to_return(body: '{"results":[]}')
    stub_request(:get, %r{\Ahttps://icanhazdadjoke\.com/search\?.*term=dog}).to_return(body: '{"results":[{"id":"d1","joke":"A dog joke."}]}')
    stub_request(:get, "https://icanhazdadjoke.com/").to_return(body: '{"id":"r1","joke":"A random dad joke."}')
  end

  def llm = AgentLab::Ollama::Client.new(base_url: nil)
  def jokes = AgentLab::JokeApi::Client.new
  def dads = AgentLab::DadJoke::Client.new
end

Minitest::Test.include(Fakes)
