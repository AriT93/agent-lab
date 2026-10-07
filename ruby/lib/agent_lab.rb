# frozen_string_literal: true

# agent-lab, in Ruby. Each stage teaches one LLM/agent concept with a tiny
# domain (ask for a joke, fetch one from an API). See ../README.md for the
# stage table; this directory mirrors the Go stages, file for file.
module AgentLab
  autoload :Http,     "agent_lab/http"
  autoload :Trace,    "agent_lab/trace"
  autoload :JokeApi,  "agent_lab/joke_api"
  autoload :DadJoke,  "agent_lab/dad_joke"
  autoload :Ollama,   "agent_lab/ollama"
  autoload :Stage0,   "agent_lab/stage0"
  autoload :Stage1,   "agent_lab/stage1"
  autoload :Stage2,   "agent_lab/stage2"
  autoload :Stage3,   "agent_lab/stage3"
  autoload :Stage3b,  "agent_lab/stage3b"
  autoload :Stage8,   "agent_lab/stage8"
  autoload :Stage4,   "agent_lab/stage4"
  autoload :Evals,    "agent_lab/evals"
  autoload :Web,      "agent_lab/web"
end
