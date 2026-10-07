# frozen_string_literal: true

require "test_helper"

class EvalsTest < Minitest::Test
  C = AgentLab::Evals::Checks
  R = AgentLab::JokeApi::Request
  Call = AgentLab::Evals::Call
  Outcome = AgentLab::Evals::Outcome

  def test_request_checks
    assert_nil C.has_categories("Dark", "Pun").call(R.new(categories: %w[Pun Dark]))
    refute_nil C.has_categories("Dark").call(R.new)
    assert_nil C.blacklists_all.call(R.new(blacklist: AgentLab::JokeApi::FLAGS))
    assert_match(/missing/, C.blacklists("nsfw").call(R.new))
    assert_match(/allowed/, C.allows("nsfw").call(R.new(blacklist: ["nsfw"])))
    assert_nil C.contains_word("dog", "dogs").call(R.new(contains: " Dog "))
  end

  def test_outcome_checks
    joke_call = Call.new("get_joke", { blacklist: ["political"] }, '{"joke":"Why?"}')
    o = Outcome.new("Why?", [joke_call])
    assert_nil C.uses_tool("get_joke").call(o)
    assert_match(/never|expected/, C.uses_tool("search_dad_jokes").call(o))
    assert_match(/called tools/, C.no_tools.call(o))
    assert_nil C.reply_contains_tool_joke.call(o)
    assert_match(/does not contain/, C.reply_contains_tool_joke.call(Outcome.new("nope", [joke_call])))
    assert_nil C.every_call("get_joke", C.blacklists("political")).call(o)
    assert_match(/missing/, C.every_call("get_joke", C.blacklists("nsfw")).call(o))
    assert_match(/explained unasked/, C.short_reply(10).call(Outcome.new("x" * 100, [joke_call])))
  end

  def test_run_requests_report_and_tally
    cases = AgentLab::Evals::Cases::REQUESTS
    results = AgentLab::Evals.run_requests(AgentLab::Stage0::Interpreter.new, cases, filter: /category|clean/)
    assert_equal %w[category clean clean-and-category], results.map(&:name)
    io = StringIO.new
    assert_equal 1, AgentLab::Evals.report(results, io: io)
    assert_match(/1\/3 passed/, io.string)
    names, passes = AgentLab::Evals.tally([results, results])
    assert_equal 2, passes["category"]
    assert_equal results.map(&:name), names
  end

  def test_run_conversations_records_tool_calls
    fake_sources
    script_model(call("search_dad_jokes", { term: "dog" }), say("A dog joke."))
    new_agent = lambda do |record|
      AgentLab::Stage3::Agent.new(llm, jokes, dads).tap { |a| a.on_tool = ->(n, args, res) { record.call(Call.new(n, args, res)) } }
    end
    kase = AgentLab::Evals::ConversationCase.new("c", [AgentLab::Evals::Turn.new("dog joke", [C.uses_tool("search_dad_jokes"), C.reply_contains_tool_joke])])
    result = AgentLab::Evals.run_conversations(new_agent, [kase]).first
    assert result.passed?, result.failures.inspect
  end
end
