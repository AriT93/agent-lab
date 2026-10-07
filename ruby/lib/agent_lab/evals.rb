# frozen_string_literal: true

require "time"

module AgentLab
  # Evals score the stages against a fixed table of prompts.
  #
  # Lesson: "it seemed to work when I tried it" doesn't survive a prompt change
  # or a model swap. An eval turns the behaviour you care about into cases with
  # a pass/fail check, so you can change a prompt, a schema or a model and see
  # what moved. Two kinds of case:
  #
  # * Request cases (stages 0-1): one prompt -> the JokeApi::Request an
  #   interpreter produced. Checked on the typed request, not on text.
  # * Conversation cases (stage 3): several user messages in a row. Checked on
  #   which tools the agent called and with what, plus the reply text.
  #
  # A check is a lambda returning nil for a pass or a reason (String) for a
  # fail. Model output varies from run to run, so repeat each case (-n) and
  # read the pass rate rather than trusting one run.
  module Evals
    autoload :Checks, "agent_lab/evals/checks"
    autoload :Cases,  "agent_lab/evals/cases"

    RequestCase = Struct.new(:name, :prompt, :checks)
    # say: one user message; checks run on the Outcome it produced.
    Turn = Struct.new(:say, :checks)
    ConversationCase = Struct.new(:name, :turns)
    # One tool call the agent made.
    Call = Struct.new(:tool, :args, :result)
    # What the agent did in response to one Turn.
    Outcome = Struct.new(:reply, :calls)
    # failures empty and error nil means pass; error means the stage itself
    # failed (network, bad model output).
    Result = Struct.new(:name, :failures, :error, :took) do
      def passed? = error.nil? && failures.empty?
    end

    module_function

    # Runs every case whose name matches filter (nil = all).
    def run_requests(interpreter, cases, filter: nil)
      select(cases, filter).map do |c|
        timed(c.name) do |res|
          req = interpreter.interpret(c.prompt)
          res.failures = c.checks.filter_map { |check| check.call(req) }
        end
      end
    end

    # Runs every case whose name matches filter, each on a fresh agent.
    # new_agent receives a recorder and must report the agent's tool calls to it.
    def run_conversations(new_agent, cases, filter: nil)
      select(cases, filter).map do |c|
        timed(c.name) do |res|
          calls = []
          agent = new_agent.call(->(call) { calls << call })
          c.turns.each_with_index do |turn, i|
            calls.clear
            begin
              reply = agent.respond(turn.say)
            rescue StandardError => e
              raise e.class, "turn #{i + 1} (#{turn.say.inspect}): #{e.message}"
            end
            outcome = Outcome.new(reply, calls.dup)
            turn.checks.each do |check|
              why = check.call(outcome)
              res.failures << "turn #{i + 1} (#{turn.say.inspect}): #{why}" if why
            end
          end
        end
      end
    end

    # Prints one line per result and a total; returns the pass count.
    def report(results, io: $stdout)
      results.each do |r|
        mark = r.error ? "ERR " : (r.passed? ? "PASS" : "FAIL")
        io.puts format("%-4s  %-34s %6dms", mark, r.name, (r.took * 1000).round)
        io.puts "      error: #{r.error}" if r.error
        r.failures.each { |f| io.puts "      - #{f}" }
      end
      passed = results.count(&:passed?)
      io.puts "\n#{passed}/#{results.size} passed"
      passed
    end

    # Sums repeated runs of the same cases into pass counts per case name.
    def tally(runs)
      names = runs.first.map(&:name)
      passes = Hash.new(0)
      runs.each { |run| run.each { |r| passes[r.name] += 1 if r.passed? } }
      [names, passes]
    end

    def select(cases, filter) = filter ? cases.select { |c| c.name.match?(filter) } : cases

    def timed(name)
      res = Result.new(name, [], nil, 0)
      start = Process.clock_gettime(Process::CLOCK_MONOTONIC)
      begin
        yield res
      rescue Ollama::Error, JokeApi::Error, DadJoke::Error, Http::Error => e
        res.error = e.message
      end
      res.took = Process.clock_gettime(Process::CLOCK_MONOTONIC) - start
      res
    end
  end
end
