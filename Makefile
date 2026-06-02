BINARY  = bin/go-code
SERVICE = go-code
COMPOSE = cd $(HOME)/deploy/example-server && docker compose

.PHONY: build lint fmt-check test run deploy clean vendor

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
