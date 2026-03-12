# FlowForge Makefile
# ==============================================================================

# Build variables
VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME   ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GO_MODULE    := github.com/kasidit-wansudon/flowforge
LDFLAGS      := -s -w \
                -X main.version=$(VERSION) \
                -X main.buildTime=$(BUILD_TIME) \
                -X main.gitCommit=$(GIT_COMMIT)

# Go variables
GOBIN        ?= $(shell go env GOPATH)/bin
CGO_ENABLED  ?= 0

# Docker variables
DOCKER_REGISTRY ?= ghcr.io
DOCKER_REPO     ?= $(DOCKER_REGISTRY)/kasidit-wansudon/flowforge
DOCKER_TAG      ?= $(VERSION)
PLATFORMS       ?= linux/amd64,linux/arm64

# Directories
BIN_DIR      := bin
DIST_DIR     := dist
PROTO_DIR    := proto
GEN_DIR      := gen

# Binaries to build
BINARIES     := server worker cli migrate

# Colors for terminal output
CYAN  := \033[36m
GREEN := \033[32m
RESET := \033[0m

.PHONY: all build test lint fmt clean help
.DEFAULT_GOAL := help

# ==============================================================================
# Build
# ==============================================================================

## build: Build all binaries for current platform
build: $(addprefix build-,$(BINARIES))

build-%:
	@printf "$(CYAN)Building $*...$(RESET)\n"
	CGO_ENABLED=$(CGO_ENABLED) go build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$* ./cmd/$*/

## build-all: Build all binaries for all platforms (linux, darwin; amd64, arm64)
build-all:
	@for os in linux darwin; do \
		for arch in amd64 arm64; do \
			printf "$(CYAN)Building for $$os/$$arch...$(RESET)\n"; \
			for bin in $(BINARIES); do \
				GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
					go build -ldflags="$(LDFLAGS)" \
					-o $(DIST_DIR)/$$os-$$arch/$$bin ./cmd/$$bin/; \
			done; \
		done; \
	done
	@printf "$(GREEN)All binaries built in $(DIST_DIR)/$(RESET)\n"

## install: Install all binaries to GOPATH/bin
install: $(addprefix install-,$(BINARIES))

install-%:
	CGO_ENABLED=$(CGO_ENABLED) go install -ldflags="$(LDFLAGS)" ./cmd/$*/

# ==============================================================================
# Test
# ==============================================================================

## test: Run all tests
test:
	@printf "$(CYAN)Running tests...$(RESET)\n"
	go test -race -count=1 ./...

## test-short: Run tests in short mode (skip integration tests)
test-short:
	@printf "$(CYAN)Running short tests...$(RESET)\n"
	go test -race -short ./...

## test-integration: Run only integration tests
test-integration:
	@printf "$(CYAN)Running integration tests...$(RESET)\n"
	go test -race -run Integration -count=1 ./...

## test-coverage: Run tests with coverage report
test-coverage:
	@printf "$(CYAN)Running tests with coverage...$(RESET)\n"
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@printf "$(GREEN)Coverage report: coverage.html$(RESET)\n"

## test-bench: Run benchmarks
test-bench:
	@printf "$(CYAN)Running benchmarks...$(RESET)\n"
	go test -bench=. -benchmem -run=^$$ ./...

## test-frontend: Run frontend tests
test-frontend:
	@printf "$(CYAN)Running frontend tests...$(RESET)\n"
	cd frontend && npm test -- --run

## test-python: Run Python SDK tests
test-python:
	@printf "$(CYAN)Running Python SDK tests...$(RESET)\n"
	cd sdk/python && pip install -e ".[dev]" && pytest -v

# ==============================================================================
# Lint & Format
# ==============================================================================

## lint: Run all linters
lint: lint-go lint-proto lint-frontend lint-python

## lint-go: Run Go linter
lint-go:
	@printf "$(CYAN)Running golangci-lint...$(RESET)\n"
	golangci-lint run --timeout=5m ./...

## lint-proto: Run protobuf linter
lint-proto:
	@printf "$(CYAN)Running buf lint...$(RESET)\n"
	buf lint $(PROTO_DIR)/

## lint-frontend: Run frontend linter
lint-frontend:
	@printf "$(CYAN)Running frontend lint...$(RESET)\n"
	cd frontend && npm run lint

## lint-python: Run Python linter
lint-python:
	@printf "$(CYAN)Running ruff...$(RESET)\n"
	cd sdk/python && ruff check . && ruff format --check .

## fmt: Format all code
fmt: fmt-go fmt-proto fmt-frontend fmt-python

## fmt-go: Format Go code
fmt-go:
	@printf "$(CYAN)Formatting Go code...$(RESET)\n"
	gofmt -w .
	goimports -w .

## fmt-proto: Format protobuf files
fmt-proto:
	@printf "$(CYAN)Formatting proto files...$(RESET)\n"
	buf format $(PROTO_DIR)/ -w

## fmt-frontend: Format frontend code
fmt-frontend:
	@printf "$(CYAN)Formatting frontend code...$(RESET)\n"
	cd frontend && npx prettier --write "src/**/*.{ts,tsx,css}"

## fmt-python: Format Python code
fmt-python:
	@printf "$(CYAN)Formatting Python code...$(RESET)\n"
	cd sdk/python && ruff format . && ruff check --fix .

## vet: Run go vet
vet:
	@printf "$(CYAN)Running go vet...$(RESET)\n"
	go vet ./...

# ==============================================================================
# Code Generation
# ==============================================================================

## proto-gen: Generate Go code from protobuf definitions
proto-gen:
	@printf "$(CYAN)Generating protobuf code...$(RESET)\n"
	@mkdir -p $(GEN_DIR)/proto
	buf generate $(PROTO_DIR)/
	@printf "$(GREEN)Proto generation complete$(RESET)\n"

## proto-breaking: Check for breaking protobuf changes
proto-breaking:
	@printf "$(CYAN)Checking for breaking changes...$(RESET)\n"
	buf breaking $(PROTO_DIR)/ --against '.git#branch=main,subdir=$(PROTO_DIR)/'

## generate: Run all code generation
generate: proto-gen
	@printf "$(CYAN)Running go generate...$(RESET)\n"
	go generate ./...

# ==============================================================================
# Database
# ==============================================================================

## db-migrate: Run database migrations
db-migrate:
	@printf "$(CYAN)Running database migrations...$(RESET)\n"
	$(BIN_DIR)/migrate --database-url="$(DATABASE_URL)" up

## db-rollback: Rollback last database migration
db-rollback:
	@printf "$(CYAN)Rolling back last migration...$(RESET)\n"
	$(BIN_DIR)/migrate --database-url="$(DATABASE_URL)" down 1

## db-reset: Reset database (drop and recreate)
db-reset:
	@printf "$(CYAN)Resetting database...$(RESET)\n"
	$(BIN_DIR)/migrate --database-url="$(DATABASE_URL)" drop -f
	$(BIN_DIR)/migrate --database-url="$(DATABASE_URL)" up

## db-status: Show migration status
db-status:
	@printf "$(CYAN)Migration status:$(RESET)\n"
	$(BIN_DIR)/migrate --database-url="$(DATABASE_URL)" version

# ==============================================================================
# Docker
# ==============================================================================

## docker-build: Build Docker images for current platform
docker-build: docker-build-server docker-build-worker docker-build-cli

docker-build-%:
	@printf "$(CYAN)Building Docker image: $*...$(RESET)\n"
	docker build \
		--target $* \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		-t $(DOCKER_REPO)-$*:$(DOCKER_TAG) \
		-t $(DOCKER_REPO)-$*:latest \
		.

## docker-build-multiarch: Build multi-architecture Docker images
docker-build-multiarch:
	@for target in server worker cli; do \
		printf "$(CYAN)Building multi-arch image: $$target...$(RESET)\n"; \
		docker buildx build \
			--target $$target \
			--platform $(PLATFORMS) \
			--build-arg VERSION=$(VERSION) \
			--build-arg BUILD_TIME=$(BUILD_TIME) \
			--build-arg GIT_COMMIT=$(GIT_COMMIT) \
			-t $(DOCKER_REPO)-$$target:$(DOCKER_TAG) \
			-t $(DOCKER_REPO)-$$target:latest \
			--push \
			.; \
	done

## docker-push: Push Docker images to registry
docker-push:
	@for target in server worker cli; do \
		printf "$(CYAN)Pushing: $$target...$(RESET)\n"; \
		docker push $(DOCKER_REPO)-$$target:$(DOCKER_TAG); \
		docker push $(DOCKER_REPO)-$$target:latest; \
	done

## docker-up: Start all services with docker compose
docker-up:
	@printf "$(CYAN)Starting services...$(RESET)\n"
	docker compose up -d

## docker-down: Stop all services
docker-down:
	@printf "$(CYAN)Stopping services...$(RESET)\n"
	docker compose down

## docker-logs: Follow docker compose logs
docker-logs:
	docker compose logs -f

## docker-ps: Show running containers
docker-ps:
	docker compose ps

## docker-clean: Remove all containers, volumes, and images
docker-clean:
	@printf "$(CYAN)Cleaning up Docker resources...$(RESET)\n"
	docker compose down -v --rmi local

# ==============================================================================
# Helm
# ==============================================================================

## helm-lint: Lint Helm chart
helm-lint:
	@printf "$(CYAN)Linting Helm chart...$(RESET)\n"
	helm lint deploy/helm/flowforge/

## helm-template: Render Helm chart templates
helm-template:
	@printf "$(CYAN)Rendering Helm templates...$(RESET)\n"
	helm template flowforge deploy/helm/flowforge/ \
		--set secrets.jwtSecret=test-jwt-secret \
		--set secrets.encryptionKey=test-encryption-key \
		--set secrets.postgres.password=test-password

## helm-package: Package Helm chart
helm-package:
	@printf "$(CYAN)Packaging Helm chart...$(RESET)\n"
	helm dependency update deploy/helm/flowforge/
	helm package deploy/helm/flowforge/ -d $(DIST_DIR)/helm/

## helm-install: Install Helm chart to current cluster
helm-install:
	@printf "$(CYAN)Installing FlowForge via Helm...$(RESET)\n"
	helm upgrade --install flowforge deploy/helm/flowforge/ \
		--namespace flowforge \
		--create-namespace \
		--wait

## helm-uninstall: Uninstall Helm release
helm-uninstall:
	@printf "$(CYAN)Uninstalling FlowForge...$(RESET)\n"
	helm uninstall flowforge --namespace flowforge

# ==============================================================================
# Development
# ==============================================================================

## dev: Start development environment (docker compose + hot reload)
dev:
	@printf "$(CYAN)Starting development environment...$(RESET)\n"
	docker compose up -d postgres redis nats
	@printf "$(GREEN)Infrastructure services started.$(RESET)\n"
	@printf "$(CYAN)Run 'make run-server' and 'make run-worker' in separate terminals.$(RESET)\n"

## run-server: Run the server locally
run-server: build-server
	@printf "$(CYAN)Starting server...$(RESET)\n"
	$(BIN_DIR)/server

## run-worker: Run a worker locally
run-worker: build-worker
	@printf "$(CYAN)Starting worker...$(RESET)\n"
	$(BIN_DIR)/worker

## run-frontend: Start frontend dev server
run-frontend:
	@printf "$(CYAN)Starting frontend dev server...$(RESET)\n"
	cd frontend && npm run dev

# ==============================================================================
# Security
# ==============================================================================

## security: Run all security checks
security: security-vulncheck security-gosec security-trivy

## security-vulncheck: Run Go vulnerability check
security-vulncheck:
	@printf "$(CYAN)Running govulncheck...$(RESET)\n"
	govulncheck ./...

## security-gosec: Run Go security linter
security-gosec:
	@printf "$(CYAN)Running gosec...$(RESET)\n"
	gosec -severity medium ./...

## security-trivy: Run Trivy filesystem scan
security-trivy:
	@printf "$(CYAN)Running Trivy scan...$(RESET)\n"
	trivy fs --severity HIGH,CRITICAL .

# ==============================================================================
# Tools
# ==============================================================================

## tools: Install development tools
tools:
	@printf "$(CYAN)Installing development tools...$(RESET)\n"
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.57.2
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/securego/gosec/v2/cmd/gosec@latest
	go install github.com/bufbuild/buf/cmd/buf@v1.30.0
	@printf "$(GREEN)Tools installed$(RESET)\n"

# ==============================================================================
# Cleanup
# ==============================================================================

## clean: Remove build artifacts
clean:
	@printf "$(CYAN)Cleaning build artifacts...$(RESET)\n"
	rm -rf $(BIN_DIR) $(DIST_DIR) $(GEN_DIR)
	rm -f coverage.out coverage.html
	@printf "$(GREEN)Clean$(RESET)\n"

# ==============================================================================
# CI targets (used by GitHub Actions)
# ==============================================================================

## ci: Run the full CI pipeline locally
ci: lint test-short build docker-build helm-lint
	@printf "$(GREEN)CI pipeline passed$(RESET)\n"

## ci-full: Run the full CI pipeline with integration tests
ci-full: lint test build docker-build helm-lint security
	@printf "$(GREEN)Full CI pipeline passed$(RESET)\n"

# ==============================================================================
# Help
# ==============================================================================

## help: Print this help message
help:
	@printf "\n$(CYAN)FlowForge$(RESET) — Distributed Workflow Orchestration Engine\n\n"
	@printf "$(GREEN)Usage:$(RESET)\n"
	@printf "  make $(CYAN)<target>$(RESET)\n\n"
	@printf "$(GREEN)Targets:$(RESET)\n"
	@grep -E '^## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ": "}; {printf "  $(CYAN)%-28s$(RESET) %s\n", $$1, $$2}' | \
		sed 's/## //'
	@printf "\n"
