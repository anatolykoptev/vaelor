#include <stdio.h>
#include "config.h"

typedef struct {
    char* host;
    int port;
} Config;

struct Server {
    Config config;
    int running;
};

enum Status {
    ACTIVE,
    INACTIVE
};

Config* create_config(const char* host, int port);

void run_server(Config* config) {
    printf("%s:%d\n", config->host, config->port);
}
