<?php

use App\Models\User;

const MAX_RETRIES = 3;

function create_config(string $host, int $port): array {
    return ['host' => $host, 'port' => $port];
}

interface Handler {
    public function handle($request);
}

trait Loggable {
    public function log(string $message): void {
        echo $message;
    }
}

class Config implements Handler {
    use Loggable;

    private string $host;
    private int $port;

    public function __construct(string $host, int $port) {
        $this->host = $host;
        $this->port = $port;
    }

    public function address(): string {
        return $this->host . ':' . $this->port;
    }

    public function handle($request) {
        $this->log('handling');
    }
}
