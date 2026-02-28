BINARY  = bin/go-code
SERVICE = go-code
COMPOSE = cd $(HOME)/deploy/example-server && docker compose

.PHONY: build lint test run deploy clean

build:
	CGO_ENABLED=1 go build -o $(BINARY) ./cmd/go-code

lint:
	golangci-lint run ./...

test:
	go test ./...

run: build
	./$(BINARY)

deploy:
	$(COMPOSE) build --no-cache $(SERVICE)
	$(COMPOSE) up -d --no-deps --force-recreate $(SERVICE)
	@echo "Deployed and restarted $(SERVICE)"

clean:
	rm -f $(BINARY)
