#include <iostream>
#include <string>

struct Point {
    double x;
    double y;
};

class Config {
public:
    Config(const std::string& host, int port);
    std::string address() const;

private:
    std::string host_;
    int port_;
};

Config::Config(const std::string& host, int port)
    : host_(host), port_(port) {}

std::string Config::address() const {
    return host_ + ":" + std::to_string(port_);
}

enum Status {
    ACTIVE,
    INACTIVE
};

void run(const Config& config) {
    std::cout << config.address() << std::endl;
}
