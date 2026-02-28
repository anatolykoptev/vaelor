require 'json'
require_relative 'config'

MAX_RETRIES = 3

module Server
  class Config
    def initialize(host, port)
      @host = host
      @port = port
    end

    def address
      "#{@host}:#{@port}"
    end

    def self.default
      new("localhost", 8080)
    end
  end
end

def create_config
  Server::Config.new("localhost", 8080)
end
