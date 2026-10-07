# frozen_string_literal: true

# `bundle exec rackup` (or bin/jokes-web, which adds options and cleanup).
$LOAD_PATH.unshift File.expand_path("lib", __dir__)
require "agent_lab"

AgentLab::Web.set :llm, AgentLab::Ollama::Client.new
AgentLab::Web.set :jokes, AgentLab::JokeApi::Client.new
AgentLab::Web.set :dads, AgentLab::DadJoke::Client.new
run AgentLab::Web
