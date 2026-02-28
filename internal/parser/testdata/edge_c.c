#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Forward declarations
struct Node;
typedef void (*Callback)(int status);

// Function-like macro (should NOT be captured as function)
#define MAX(a, b) ((a) > (b) ? (a) : (b))
#define BUFFER_SIZE 1024

// Struct with function pointer members
typedef struct {
    char* name;
    int (*compare)(const void*, const void*);
    void (*destroy)(void*);
} Handler;

// Named struct
struct Node {
    int value;
    struct Node* next;
};

// Enum
enum LogLevel {
    LOG_DEBUG,
    LOG_INFO,
    LOG_ERROR
};

// Static function
static int internal_helper(int x) {
    return x * 2;
}

// Function with complex return type
struct Node* create_node(int value) {
    struct Node* n = malloc(sizeof(struct Node));
    n->value = value;
    n->next = NULL;
    return n;
}

// Variadic function
void log_message(enum LogLevel level, const char* fmt, ...) {
    // implementation
}

// Function pointer parameter
void register_callback(Callback cb) {
    cb(0);
}
