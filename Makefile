BINARY := gw2-collector
PKG := ./cmd/gw2-collector
COMPOSE := docker compose -f deploy/docker-compose.yaml

.PHONY: build run test vet tidy fmt dev dev-down clean

build: ## Build the collector binary
	CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o $(BINARY) $(PKG)

run: ## Run the collector locally (needs GW2_API_KEY in the environment)
	go run $(PKG)

test: ## Run tests
	go test ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format the code
	gofmt -w .

tidy: ## Tidy module dependencies
	go mod tidy

dev: ## Bring up the local LGTM stack + collector (needs GW2_API_KEY)
	$(COMPOSE) up --build

dev-down: ## Tear down the local dev stack
	$(COMPOSE) down

clean: ## Remove the built binary
	rm -f $(BINARY)
