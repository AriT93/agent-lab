# frozen_string_literal: true

require "json"
require "net/http"
require "uri"

module AgentLab
  # A thin wrapper over Net::HTTP so the API clients read as "send this JSON,
  # get that JSON" and nothing more. No HTTP gem on purpose: this is the whole
  # of what talking to a model server or a joke API takes.
  module Http
    class Error < StandardError; end

    Response = Struct.new(:status, :body, keyword_init: true)

    module_function

    def get(url, headers: {}, timeout: 10)
      request(Net::HTTP::Get, url, headers: headers, timeout: timeout)
    end

    def post_json(url, payload, timeout: 300)
      request(Net::HTTP::Post, url, headers: { "Content-Type" => "application/json" },
                                    body: JSON.generate(payload), timeout: timeout)
    end

    def request(klass, url, headers:, timeout:, body: nil)
      uri = URI(url)
      req = klass.new(uri, headers)
      req.body = body if body
      res = Net::HTTP.start(uri.host, uri.port, use_ssl: uri.scheme == "https",
                                                open_timeout: timeout, read_timeout: timeout) { |http| http.request(req) }
      Response.new(status: res.code.to_i, body: res.body.to_s)
    rescue SystemCallError, SocketError, Timeout::Error, OpenSSL::SSL::SSLError => e
      raise Error, e.message
    end
  end
end
