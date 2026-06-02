# Multi-stage build for go-code.
# CGO_ENABLED=1 is required for tree-sitter grammar bindings (C libraries).
# The runtime image includes git for repository cloning operations.

# ── Stage 1: Builder ─────────────────────────────────────────────────────────
FROM golang:1.26.3-alpine AS builder

# Install C toolchain for CGO (tree-sitter grammars are C libraries).
RUN apk add --no-cache gcc musl-dev git

WORKDIR /build

# Download dependencies first for layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build with version from git tag.
COPY . .
RUN VERSION=$(git describe --tags --always 2>/dev/null || echo "dev") && \
    CGO_ENABLED=1 go build -ldflags="-s -w -X main.version=${VERSION}" -o go-code ./cmd/go-code

# ── Stage 2: Runtime ─────────────────────────────────────────────────────────
FROM golang:1.26.3-alpine

# ca-certificates: HTTPS to GitHub API and LLM proxy.
# git: shallow cloning of repositories for analysis.
# tzdata: proper timezone handling in logs.
# nodejs/npm: SCIP indexers for TypeScript and Python.
# rust/cargo/rust-analyzer: rust-analyzer ships a `scip` subcommand.
# openjdk17-jre-headless: required by the scip-java launcher (JAR with embedded coursier bootstrap).
# curl: download scip-java release asset during the next layer.
RUN apk add --no-cache ca-certificates git tzdata nodejs npm rust cargo rust-analyzer openjdk17-jre-headless curl openssh-client && \
    git config --global --add safe.directory '*'

# SCIP indexers for multi-language type-aware analysis.
#
# Installed (ARM64 + x86_64):
#   - scip-typescript / scip-python via npm — TS, JS, Python.
#   - rust-analyzer via apk — Rust (`rust-analyzer scip .`).
#   - scip-java prebuilt launcher — Java/Kotlin/Scala (single arch-independent JAR launcher, runs on JVM).
#
# Intentionally absent (P3.F5 audit, 2026-05-05) — registry entries remain so
# IndexerAvailable() returns false at runtime and callers cleanly fall back to
# the basic tier instead of erroring:
#   - scip-go      : Go uses internal go/types analysis (PR #33).
#   - scip-ruby    : upstream ships only x86_64-linux + arm64-darwin (no linux/aarch64).
#   - scip-clang   : upstream ships only x86_64-linux + arm64-darwin (no linux/aarch64).
#   - scip-dotnet  : upstream distributes only as a Docker image (no release asset binaries).
ARG SCIP_JAVA_VERSION=v0.12.3
ARG SCIP_JAVA_SHA256=2d4d8a31333dfa0daf3aa0381a51de465e40b0dac5622e49363786a65f743f34

RUN npm install -g @sourcegraph/scip-typescript @sourcegraph/scip-python && \
    npm cache clean --force && \
    curl -fsSL --retry 3 --retry-delay 5 \
        "https://github.com/sourcegraph/scip-java/releases/download/${SCIP_JAVA_VERSION}/scip-java-${SCIP_JAVA_VERSION}" \
        -o /usr/local/bin/scip-java && \
    echo "${SCIP_JAVA_SHA256}  /usr/local/bin/scip-java" | sha256sum -c - && \
    chmod +x /usr/local/bin/scip-java

WORKDIR /app
COPY --from=builder /build/go-code .

EXPOSE 8897

CMD ["./go-code"]
