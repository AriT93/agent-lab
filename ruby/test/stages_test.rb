# frozen_string_literal: true

require "test_helper"
require "langchain" # the 3b tests touch Langchain.logger before the stage autoloads it

class Stage0Test < Minitest::Test
  def test_keywords
    r = AgentLab::Stage0::Interpreter.new.interpret("a clean programming one-liner, no racist")
    assert_equal ["Programming"], r.categories
    assert_equal "single", r.type
    assert_equal %w[racist nsfw], r.blacklist
  end
end

class Stage1Test < Minitest::Test
  def test_structured_output_is_requested_and_normalized
    requests = script_model(say('{"categories":["programming"],"type":"any","blacklist":["NSFW"],"contains":""}'))
    r = AgentLab::Stage1::Interpreter.new(llm).interpret("a clean programming joke")
    assert_equal ["Programming"], r.categories
    assert_equal ["nsfw"], r.blacklist
    assert_equal AgentLab::JokeApi.schema.to_json, requests.first[:format].to_json
    assert_equal false, requests.first[:think]
  end

  def test_bad_output
    script_model(say("not json"))
    assert_raises(AgentLab::Ollama::Error) { AgentLab::Stage1::Interpreter.new(llm).interpret("x") }
  end
end

class Stage2Test < Minitest::Test
  def test_tool_loop_retries_after_no_match
    requests = script_model(
      call("get_joke", { categories: [], type: "any", blacklist: [], contains: "zzz" }),
      call("get_joke", { categories: [], type: "any", blacklist: [], contains: "" }),
      say("The only JokeAPI joke.")
    )
    stub_request(:get, "https://v2.jokeapi.dev/joke/Any?contains=zzz").to_return(
      body: '{"error":true,"code":106,"message":"No matching joke found","causedBy":[]}'
    )
    stub_request(:get, "https://v2.jokeapi.dev/joke/Any").to_return(
      body: '{"error":false,"type":"single","joke":"The only JokeAPI joke.","category":"Misc","id":1}'
    )
    assert_equal "The only JokeAPI joke.", AgentLab::Stage2::Agent.new(llm, jokes).respond("a joke about zzz")
    # The tool error went back to the model as a result, not an exception.
    second = requests[1][:messages].last
    assert_equal "tool", second[:role]
    assert_match(/No matching joke found/, second[:content])
  end

  def test_gives_up_after_max_steps
    script_model(*Array.new(2) { call("nope", {}) })
    e = assert_raises(AgentLab::Ollama::Error) { AgentLab::Stage2::Agent.new(llm, jokes, max_steps: 2).respond("x") }
    assert_match(/gave up after 2 steps/, e.message)
  end
end

class Stage3Test < Minitest::Test
  def agent(**opts) = AgentLab::Stage3::Agent.new(llm, jokes, dads, **opts)

  def test_tool_result_reaches_model_and_memory_persists
    fake_sources
    requests = script_model(call("search_dad_jokes", { term: "dog" }), say("A dog joke."), say("It's a pun."))
    a = agent
    calls = []
    a.on_tool = ->(name, args, result) { calls << [name, args, result] }

    assert_equal "A dog joke.", a.respond("a joke about dogs")
    assert_equal "search_dad_jokes", calls.first[0]
    assert_includes requests[1][:messages].last[:content], "A dog joke."
    assert_equal 2, requests.first[:tools].size

    a.respond("explain it")
    texts = requests[2][:messages].map { |m| m[:content] }.join(" ")
    assert_includes texts, "a joke about dogs"
    assert_includes texts, "A dog joke."
  end

  def test_repeated_joke_is_refused_and_reset_forgets
    fake_sources
    args = { categories: [], type: "any", blacklist: [], contains: "" }
    requests = script_model(call("get_joke", args), call("get_joke", args), say("done"), call("get_joke", args), say("again"))
    a = agent
    a.respond("joke")
    assert_match(/already told/, requests[2][:messages].last[:content])

    a.reset
    a.respond("joke after reset")
    assert_includes requests[4][:messages].last[:content], "The only JokeAPI joke."
  end

  def test_old_turns_are_dropped_whole
    requests = script_model(*Array.new(4) { say("ok") })
    a = agent(max_turns: 2)
    %w[one two three].each { |t| a.respond(t) }
    roles = requests[2][:messages].map { |m| m[:content] }
    refute_includes roles, "one"
    assert_includes roles, "two"
  end

  def test_unknown_tool_is_an_error_result
    requests = script_model(call("nope", {}), say("sorry"))
    agent.respond("x")
    assert_match(/unknown tool nope/, requests[1][:messages].last[:content])
  end
end

class Stage3bTest < Minitest::Test
  def agent = AgentLab::Stage3b::Agent.new(llm, jokes, dads)

  def test_tool_call_memory_and_ollama_limits
    fake_sources
    requests = script_model(call("dad_jokes__search", { term: "dog" }), say("A dog joke."), say("It's a pun."))
    a = agent
    assert_equal "A dog joke.", a.respond("a joke about dogs")

    first = requests.first
    assert_equal({ num_ctx: 8192, num_predict: 2048 }, first[:options])
    assert_equal "2m", first[:keep_alive]
    assert_equal false, first[:think]
    assert_equal %w[dad_jokes__search joke_api__get_joke], first[:tools].map { |t| t.dig(:function, :name) }.sort
    assert_includes requests[1][:messages].last[:content], "A dog joke."

    a.respond("explain it")
    texts = requests[2][:messages].map { |m| m[:content] }.join(" ")
    assert_includes texts, "a joke about dogs"
  end

  def test_tool_errors_and_repeats_are_results_not_exceptions
    fake_sources
    args = { categories: [], type: "any", blacklist: [], contains: "", invented: 1 }
    requests = script_model(call("joke_api__get_joke", args), call("joke_api__get_joke", args), say("done"))
    agent.respond("joke")
    assert_match(/already told/, requests[2][:messages].last[:content])
  end

  def test_step_cap_stops_a_looping_model
    fake_sources
    script_model(*Array.new(10) { call("dad_jokes__search", { term: "" }) })
    Langchain.logger.level = Logger::FATAL # the framework logs the error we raise on purpose
    e = assert_raises(AgentLab::Ollama::Error) { AgentLab::Stage3b::Agent.new(llm, jokes, dads, max_steps: 2).respond("x") }
    assert_match(/stopped without a reply/, e.message)
  ensure
    Langchain.logger.level = Logger::ERROR
  end
end
