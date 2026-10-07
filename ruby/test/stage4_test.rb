# frozen_string_literal: true

require "test_helper"
require "stringio"

class Stage4Test < Minitest::Test
  def serve(*messages)
    fake_sources
    out = StringIO.new
    AgentLab::Stage4.server(jokes, dads).serve(StringIO.new(messages.map { |m| JSON.generate(m) }.join("\n") + "\n"), out)
    out.string.lines.map { |l| JSON.parse(l, symbolize_names: true) }
  end

  def test_handshake_list_and_call
    replies = serve(
      { jsonrpc: "2.0", id: 1, method: "initialize", params: {} },
      { jsonrpc: "2.0", method: "notifications/initialized" },
      { jsonrpc: "2.0", id: 2, method: "tools/list" },
      { jsonrpc: "2.0", id: 3, method: "tools/call", params: { name: "search_dad_jokes", arguments: { term: "dog" } } }
    )
    assert_equal 3, replies.size, "notifications get no reply"
    assert_equal AgentLab::Stage4::PROTOCOL_VERSION, replies[0][:result][:protocolVersion]
    assert_equal %w[get_joke search_dad_jokes], replies[1][:result][:tools].map { |t| t[:name] }
    assert_equal "A dog joke.", replies[2][:result][:content][0][:text]
    assert_equal false, replies[2][:result][:isError]
  end

  def test_tool_failure_is_a_result_not_a_protocol_error
    r = serve({ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "search_dad_jokes", arguments: { term: "cubs" } } }).first
    assert_equal true, r[:result][:isError]
    assert_nil r[:error]
  end

  def test_protocol_errors
    unknown_tool, unknown_method = serve(
      { jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "nope" } },
      { jsonrpc: "2.0", id: 2, method: "bogus" }
    )
    assert_equal AgentLab::Stage4::CODE_INVALID_PARAMS, unknown_tool[:error][:code]
    assert_equal AgentLab::Stage4::CODE_METHOD_NOT_FOUND, unknown_method[:error][:code]
  end

  def test_parse_error
    out = StringIO.new
    AgentLab::Stage4.server(jokes, dads).serve(StringIO.new("{oops\n"), out)
    assert_equal AgentLab::Stage4::CODE_PARSE, JSON.parse(out.string)["error"]["code"]
  end
end
