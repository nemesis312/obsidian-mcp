.PHONY: build test clean install release snapshot help

# Binary name
BINARY=obsidian-mcp
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Build flags
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

help: ## Display this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary
	@echo "Building $(BINARY) version $(VERSION)..."
	@go build $(LDFLAGS) -o $(BINARY) ./cmd/obsidian-mcp

test: ## Run tests
	@echo "Running tests..."
	@go test -v -race ./...

coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	@go test -v -race -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report written to coverage.html"

clean: ## Remove build artifacts
	@echo "Cleaning..."
	@rm -f $(BINARY)
	@rm -f coverage.out coverage.html
	@rm -rf dist/

install: build ## Install binary to GOPATH/bin
	@echo "Installing $(BINARY)..."
	@go install $(LDFLAGS) ./cmd/obsidian-mcp

release: ## Create a release build (requires goreleaser)
	@echo "Creating release..."
	@goreleaser release --clean

snapshot: ## Create a snapshot release
	@echo "Creating snapshot..."
	@goreleaser release --snapshot --clean

lint: ## Run linters
	@echo "Running linters..."
	@go vet ./...
	@go fmt ./...

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

.DEFAULT_GOAL := help
