# frozen_string_literal: true

require "test_helper"

class JokeApiTest < Minitest::Test
  R = AgentLab::JokeApi::Request

  def test_url
    r = R.new(categories: %w[Programming Pun], type: "single", blacklist: %w[nsfw racist], contains: "bug")
    assert_equal "https://x/joke/Programming,Pun?type=single&blacklistFlags=nsfw%2Cracist&contains=bug", r.url("https://x/joke/")
    assert_equal "https://x/Any", R.new.url("https://x")
  end

  def test_normalize_drops_unknown_and_duplicates
    r = R.from_h("categories" => %w[programming Nope PROGRAMMING], "type" => "any", "blacklist" => ["NSFW", "bogus"], "contains" => " bug ")
    n = r.normalize
    assert_equal ["Programming"], n.categories
    assert_equal "", n.type
    assert_equal ["nsfw"], n.blacklist
    assert_equal "bug", n.contains
  end

  def test_fetch_and_text
    stub_request(:get, "https://v2.jokeapi.dev/joke/Any").to_return(
      body: '{"error":false,"type":"twopart","setup":"Knock knock","delivery":"Who?","category":"Misc","id":7}'
    )
    j = jokes.fetch(R.new)
    assert_equal 7, j.id
    assert_equal "Knock knock\n\nWho?", j.text
  end

  def test_api_error_even_with_non_2xx_status
    stub_request(:get, "https://v2.jokeapi.dev/joke/Any").to_return(
      status: 400, body: '{"error":true,"code":106,"message":"No matching joke found","causedBy":["too narrow"]}'
    )
    e = assert_raises(AgentLab::JokeApi::Error) { jokes.fetch(R.new) }
    assert_match(/No matching joke found \(code 106\): too narrow/, e.message)
  end

  def test_network_failure
    stub_request(:get, "https://v2.jokeapi.dev/joke/Any").to_timeout
    assert_raises(AgentLab::Http::Error) { jokes.fetch(R.new) }
  end
end

class DadJokeTest < Minitest::Test
  def test_random_search_and_no_match
    fake_sources
    assert_equal "r1", dads.random.id
    assert_equal "d1", dads.search("dog").id
    assert_raises(AgentLab::DadJoke::NoMatch) { dads.search("cubs") }
  end

  def test_asks_for_json
    stub = stub_request(:get, "https://icanhazdadjoke.com/").with(headers: { "Accept" => "application/json" }).to_return(body: '{"id":"x","joke":"y"}')
    dads.random
    assert_requested stub
  end
end

class OllamaClientTest < Minitest::Test
  def test_limits_are_sent_on_every_request
    requests = script_model(say("hi"))
    llm.chat(messages: [{ role: "user", content: "x" }], think: false)
    body = requests.first
    assert_equal({ num_ctx: 8192, num_predict: 2048 }, body[:options])
    assert_equal "2m", body[:keep_alive]
    assert_equal false, body[:think]
    refute body[:stream]
  end

  def test_error_message_from_server
    stub_request(:post, Fakes::OLLAMA).to_return(status: 404, body: '{"error":"model not found"}')
    e = assert_raises(AgentLab::Ollama::Error) { llm.chat(messages: []) }
    assert_equal "ollama: model not found", e.message
  end

  def test_unload
    stub = stub_request(:post, Fakes::OLLAMA).with { |r| JSON.parse(r.body)["keep_alive"] == "0s" }.to_return(body: "{}")
    llm.unload
    assert_requested stub
  end
end
