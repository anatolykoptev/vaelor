BINARY  = bin/go-code
SERVICE = go-code
COMPOSE = cd $(HOME)/deploy/example-server && docker compose

.PHONY: build lint fmt-check test preflight run deploy clean vendor

build:
	GOWORK=off CGO_ENABLED=1 go build -o $(BINARY) ./cmd/go-code

fmt-check:
	@out=$$(GOWORK=off gofmt -l cmd internal eval); \
	if [ -n "$$out" ]; then \
	  echo "gofmt drift detected:" >&2; \
	  echo "$$out" >&2; \
	  exit 1; \
	fi

lint: fmt-check
	GOWORK=off golangci-lint run ./...

test:
	GOWORK=off go test ./...

# preflight — the merge gate: gofmt + vet + build + test, run from
# .github/workflows/preflight.yml on the self-hosted krolik-go-code runner
# (see CLAUDE.md ## CI). go-code persists three pgvector stores
# (internal/embeddings, internal/designmd, internal/learnings) plus an Apache
# AGE property graph (internal/codegraph) behind DATABASE_URL /
# PR_TEST_DATABASE_URL; every DB-gated test t.Skip's cleanly when its var is
# unset (plain local `make preflight` with no DB configured stays green),
# and runs live once the workflow provisions an ephemeral
# krolik-postgres-age:17 container and exports both vars. No separate
# migration step is needed — each store bootstraps its own schema/extension
# on first use (EnsureSchema / EnsureGraph).
preflight: fmt-check
	@echo "==> go vet ./..."
	GOWORK=off go vet ./...
	@echo "==> go build ./..."
	GOWORK=off CGO_ENABLED=1 go build ./...
	@echo "==> go test ./..."
	$(MAKE) test

run: build
	./$(BINARY)

deploy:
	$(COMPOSE) build --no-cache $(SERVICE)
	$(COMPOSE) up -d --no-deps --force-recreate $(SERVICE)
	@echo "Deployed and restarted $(SERVICE)"

vendor:
	GOWORK=off go mod vendor
	@# go mod vendor strips C headers needed by tree-sitter CGO. Restore from
	@# GOMODCACHE — `git checkout` fallback fails when headers are absent from
	@# HEAD (recurring footgun on every migration that didn't pre-restore).
	@MOD_CACHE=$$(GOWORK=off go env GOMODCACHE); \
	HDR_DIR=vendor/github.com/smacker/go-tree-sitter/php/tree_sitter; \
	mkdir -p $$HDR_DIR && \
	cp $$MOD_CACHE/github.com/smacker/go-tree-sitter@*/php/tree_sitter/*.h $$HDR_DIR/ && \
	chmod u+w $$HDR_DIR/*.h

clean:
	rm -f $(BINARY)
