#include <iostream>
#include <vector>
#include <string>

// Namespace with functions and classes
namespace net {

class Server {
public:
    Server(int port);
    void start();
    std::string address() const;
private:
    int port_;
};

// Function inside namespace
void log_message(const std::string& msg) {
    std::cout << msg << std::endl;
}

} // namespace net

// Out-of-line method definitions
net::Server::Server(int port) : port_(port) {}

void net::Server::start() {
    std::cout << "Starting server" << std::endl;
}

// Template function
template<typename T>
T max_value(T a, T b) {
    return (a > b) ? a : b;
}

// Template class
template<typename T>
class Container {
public:
    void add(T item);
    T get(int index) const;
private:
    std::vector<T> items_;
};

// Free function
int main() {
    net::Server server(8080);
    server.start();
    return 0;
}
