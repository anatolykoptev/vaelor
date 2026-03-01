# Multi-stage build for go-code.
# CGO_ENABLED=1 is required for tree-sitter grammar bindings (C libraries).
# The runtime image includes git for repository cloning operations.

# ── Stage 1: Builder ─────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

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

# ── Stage 2: Runtime ──────────────────────────────────────────────────────────
FROM alpine:3.21

# ca-certificates: HTTPS to GitHub API and LLM proxy.
# git: shallow cloning of repositories for analysis.
# tzdata: proper timezone handling in logs.
RUN apk add --no-cache ca-certificates git tzdata

WORKDIR /app
COPY --from=builder /build/go-code .

EXPOSE 8897

CMD ["./go-code"]
