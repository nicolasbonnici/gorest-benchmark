.PHONY: help test lint lint-fix build clean install

# Add Go bin to PATH for all targets
GOPATH ?= $(shell go env GOPATH)
export PATH := $(GOPATH)/bin:$(PATH)

# Default target
GOLANGCI_LINT_VERSION := v2.12.2

.DEFAULT_GOAL := help

help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

install: ## Install dependencies, dev tools, and git hooks
	@echo "[INFO] Installing development environment..."
	@echo ""
	@echo "[1/3] Installing Go dependencies..."
	@go mod download
	@go mod tidy
	@echo "✓ Dependencies installed"
	@echo ""
	@echo "[2/3] Installing development tools..."
	@if ! golangci-lint --version 2>/dev/null | grep -qE 'version v?2\.'; then \
		echo "  Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."; \
		GOWORK=off go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	fi
	@echo "✓ Development tools installed"
	@echo ""
	@echo "[3/3] Installing git hooks..."
	@bash .githooks/install.sh
	@echo ""
	@echo "✅ Installation complete! Ready to develop."
	@echo ""
	@echo "Next steps:"
	@echo "  • Run 'make test' to verify your setup"
	@echo "  • Run 'make lint' to check code quality"
	@echo "  • See 'make help' for all available commands"
test: ## Run tests
	@echo "Running tests..."
	@go test -v ./...

lint: ## Run golangci-lint (bundles staticcheck, errcheck, govet, gocyclo, misspell)
	@echo "Running golangci-lint..."
	@$$(go env GOPATH)/bin/golangci-lint run ./...

lint-fix: ## Run linter with auto-fix
	@echo "Running golangci-lint with auto-fix..."
	@$$(go env GOPATH)/bin/golangci-lint run --fix ./...

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

# ----------------------------
# Security
# ----------------------------
# Pinned so a local scan and a CI scan judge the same code the same way; an
# unpinned scanner turns a green build red on someone else's machine.
GOVULNCHECK_VERSION := v1.8.0
GITLEAKS_VERSION := v8.30.1

.PHONY: security security-tools security-sast security-vuln security-secrets

security-tools:
	@if ! command -v govulncheck >/dev/null 2>&1; then \
		echo "  Installing govulncheck $(GOVULNCHECK_VERSION)..."; \
		GOWORK=off go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION); \
	fi
	@if ! command -v gitleaks >/dev/null 2>&1; then \
		echo "  Installing gitleaks $(GITLEAKS_VERSION)..."; \
		GOWORK=off go install github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION); \
	fi

# gosec also runs as part of `make lint`; this target isolates it so a security
# regression is readable without the rest of the linter output.
security-sast:
	@echo "[INFO] gosec (static analysis)..."
	@GOWORK=off $$(go env GOPATH)/bin/golangci-lint run --enable-only=gosec ./...

security-vuln: security-tools
	@echo "[INFO] govulncheck (known CVEs, incl. stdlib)..."
	@GOWORK=off $$(go env GOPATH)/bin/govulncheck ./...

security-secrets: security-tools
	@echo "[INFO] gitleaks (secret scan)..."
	@$$(go env GOPATH)/bin/gitleaks dir . --no-banner --redact

security: security-sast security-vuln security-secrets
	@echo "[INFO] All security checks passed!"
