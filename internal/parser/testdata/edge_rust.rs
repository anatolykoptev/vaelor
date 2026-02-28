use std::io;
use std::fmt;

// Nested modules
mod inner {
    pub fn inner_function() -> i32 {
        42
    }
}

// Struct with derive
#[derive(Debug, Clone)]
struct Config {
    host: String,
    port: u16,
}

// Multiple impl blocks
impl Config {
    fn new(host: String, port: u16) -> Self {
        Config { host, port }
    }

    fn address(&self) -> String {
        format!("{}:{}", self.host, self.port)
    }
}

impl fmt::Display for Config {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}:{}", self.host, self.port)
    }
}

// Trait with default method
trait Handler {
    fn handle(&self) -> Result<(), io::Error>;

    fn name(&self) -> &str {
        "default"
    }
}

// Generic function
fn process<T: Handler>(handler: &T) -> Result<(), io::Error> {
    handler.handle()
}

// Const and static
const MAX_CONNECTIONS: u32 = 100;
static GLOBAL_CONFIG: &str = "default";

// Enum with methods
enum Status {
    Active,
    Inactive,
    Error(String),
}

impl Status {
    fn is_active(&self) -> bool {
        matches!(self, Status::Active)
    }
}

// Top-level function
fn create_config() -> Config {
    Config::new("localhost".to_string(), 8080)
}
