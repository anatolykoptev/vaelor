use std::io;
use std::collections::HashMap;

const MAX_RETRIES: i32 = 3;

static DEFAULT_PORT: u16 = 8080;

struct Config {
    host: String,
    port: u16,
}

enum Status {
    Active,
    Inactive,
}

trait Handler {
    fn handle(&self) -> Result<(), io::Error>;
}

type AliasConfig = Config;

impl Config {
    fn new(host: String, port: u16) -> Self {
        Config { host, port }
    }

    fn address(&self) -> String {
        format!("{}:{}", self.host, self.port)
    }
}

fn create_config() -> Config {
    Config::new("localhost".to_string(), 8080)
}
