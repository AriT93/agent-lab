# frozen_string_literal: true

require "json"

module AgentLab
  # Prints what each stage is doing, so you can watch the
  # request -> model -> tool -> result flow instead of guessing at it.
  #
  # Trace::OFF is a tracer that does nothing, so callers never need to check
  # whether tracing is on (Go does the same with a nil tracer).
  class Trace
    def initialize(io = nil, color: true)
      @io = io
      @color = color
    end

    # Strings print as-is; anything else is rendered as indented JSON.
    def step(name, value)
      return unless @io

      body = value.is_a?(String) ? value : JSON.pretty_generate(value)
      text = "── #{name}\n#{body}"
      @io.puts(@color ? "\e[2m#{text}\e[0m" : text)
    end

    OFF = new.freeze
  end
end
