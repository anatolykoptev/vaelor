import os
from pathlib import Path

MAX_RETRIES = 3


class Config:
    def __init__(self, host: str, port: int):
        self.host = host
        self.port = port

    def address(self) -> str:
        return f"{self.host}:{self.port}"


def create_config() -> Config:
    return Config("localhost", 8080)
