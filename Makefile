.PHONY: help test lint lint-fix build clean install

# Default target
.DEFAULT_GOAL := help

help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

install: ## Install development tools
	@echo "Installing golangci-lint..."
	@which golangci-lint > /dev/null || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin latest
	@echo "✓ Tools installed"

test: ## Run tests with coverage
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	@echo ""
	@echo "Coverage summary:"
	@go tool cover -func=coverage.out
	@rm -f coverage.out

lint: ## Run linter
	@echo "Running golangci-lint..."
	@golangci-lint run ./...

lint-fix: ## Run linter with auto-fix
	@echo "Running golangci-lint with auto-fix..."
	@golangci-lint run --fix ./...

build: ## Build the benchmark command
	@echo "Building benchmark command..."
	@mkdir -p bin
	@go build -o bin/gorest-benchmark ./cmd/main.go
	@echo "✓ Binary created: bin/gorest-benchmark"

clean: ## Clean build artifacts and caches
	@echo "Cleaning..."
	@rm -rf bin/
	@go clean -cache -testcache -modcache
	@rm -f coverage.out
	@echo "✓ Cleaned"
