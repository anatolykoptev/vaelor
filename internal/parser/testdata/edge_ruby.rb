require 'json'
require_relative 'config'

# Top-level constant
MAX_RETRIES = 3
DEFAULT_HOST = "localhost"

# Deeply nested module
module Outer
  module Inner
    class DeepConfig
      def initialize(host)
        @host = host
      end

      def address
        @host
      end
    end
  end
end

# Class with multiple method types
class Server
  def initialize(port)
    @port = port
  end

  # Instance method
  def start
    puts "Starting on #{@port}"
  end

  # Class/singleton method
  def self.create(port)
    new(port)
  end

  # Protected method
  protected

  def internal_check
    true
  end
end

# Top-level function
def helper_function
  "I'm a helper"
end

# Another top-level function
def create_server(port)
  Server.new(port)
end
