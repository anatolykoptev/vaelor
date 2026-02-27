import { Request, Response } from 'express';

interface Handler {
    handle(req: Request, res: Response): void;
}

class Server {
    private port: number;

    constructor(port: number) {
        this.port = port;
    }

    start(): void {
        console.log(`Listening on ${this.port}`);
    }
}

function createServer(port: number): Server {
    return new Server(port);
}

export const defaultPort = 8080;
