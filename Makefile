BINARY  = bin/go-code
SERVICE = go-code
COMPOSE = cd $(HOME)/deploy/example-server && docker compose

.PHONY: build lint fmt-check test govulncheck preflight run deploy clean vendor

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

# govulncheck — dependency + toolchain vulnerability scan (plan ADR 7,
# plans/go-code/2026-06-30-frontend-parse-parity-react-svelte-astro.md Phase
# 0a): later phases vendor third-party tree-sitter grammars and the plan
# claims those deps are "OSV-scanned" — this target is what actually performs
# that scan (internal/freshness/vuln_check.go queries the same vuln.go.dev
# database for OTHER repos as a go-code PRODUCT feature; it is not go-code's
# own CI, which is why nothing self-scanned go-code until now).
#
# -scan package (not the default -scan symbol) skips govulncheck's call-graph
# reachability analysis — package-level import scanning is enough for a CI
# gate and stays fast/scoped on the 4-core ARM prod box (~1s locally); -scan
# module was tried first but rejects package patterns entirely and govulncheck
# still insists on loading "." for module metadata, which fails on this repo's
# layout (no .go files at module root, only under cmd/internal/eval) — a
# govulncheck limitation, not a go-code one.
#
# Installs a pinned binary into GOBIN if the runner doesn't already have one
# cached — first run on a fresh runner bootstraps itself via `go install`
# through the same module proxy every other dependency uses; subsequent runs
# reuse the cached binary.
GOVULNCHECK_VERSION = v1.4.0

govulncheck:
	@GOBIN=$$(GOWORK=off go env GOPATH)/bin; \
	if [ ! -x "$$GOBIN/govulncheck" ]; then \
	  echo "==> installing golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)"; \
	  GOWORK=off go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION); \
	fi; \
	echo "==> govulncheck -scan package ./..."; \
	GOWORK=off "$$GOBIN/govulncheck" -scan package ./...

# preflight — the merge gate: gofmt + vet + build + test + govulncheck, run
# from .github/workflows/preflight.yml on the self-hosted ci-runner
# runner (see CLAUDE.md ## CI). go-code persists three pgvector stores
# (internal/embeddings, internal/designmd, internal/learnings) plus an Apache
# AGE property graph (internal/codegraph) behind DATABASE_URL /
# PR_TEST_DATABASE_URL; every DB-gated test t.Skip's cleanly when its var is
# unset (plain local `make preflight` with no DB configured stays green),
# and runs live once the workflow provisions an ephemeral
# deploy-postgres-age:17 container and exports both vars. No separate
# migration step is needed — each store bootstraps its own schema/extension
# on first use (EnsureSchema / EnsureGraph).
preflight: fmt-check
	@echo "==> go vet ./..."
	GOWORK=off go vet ./...
	@echo "==> go build ./..."
	GOWORK=off CGO_ENABLED=1 go build ./...
	@echo "==> go test ./..."
	$(MAKE) test
	$(MAKE) govulncheck

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
