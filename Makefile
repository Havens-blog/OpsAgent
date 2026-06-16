# Makefile for OpsAgent

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOLINT=golangci-lint

# Binary names
BINARY_NAME=opsagent
BINARY_UNIX=$(BINARY_NAME)_unix

# Build directories
CMD_DIR=./cmd
BUILD_DIR=./build

# Version information
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Go flags
GOFLAGS?=
GOFLAGS += $(LDFLAGS)

# Coverage
COVERAGE_DIR=./coverage
COVERAGE_FILE=$(COVERAGE_DIR)/coverage.out
COVERAGE_HTML=$(COVERAGE_DIR)/coverage.html

.PHONY: all
all: clean deps lint test build

## deps: Install dependencies
.PHONY: deps
deps:
	@echo "Installing dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

## build: Build all binaries
.PHONY: build
build: build-agent build-api build-bot build-cli

## build-agent: Build agent service
.PHONY: build-agent
build-agent:
	@echo "Building agent service..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-agent $(CMD_DIR)/agent

## build-api: Build API service
.PHONY: build-api
build-api:
	@echo "Building API service..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-api $(CMD_DIR)/api

## build-bot: Build bot service
.PHONY: build-bot
build-bot:
	@echo "Building bot service..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-bot $(CMD_DIR)/bot

## build-cli: Build CLI tool
.PHONY: build-cli
build-cli:
	@echo "Building CLI tool..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)/cli

## clean: Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	@rm -rf $(BUILD_DIR)
	@rm -rf $(COVERAGE_DIR)

## test: Run all tests
.PHONY: test
test:
	@echo "Running tests..."
	@mkdir -p $(COVERAGE_DIR)
	$(GOTEST) -v -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...

## test-cov: Run tests with coverage
.PHONY: test-cov
test-cov: test
	@echo "Generating coverage report..."
	$(GOCMD) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Coverage report: $(COVERAGE_HTML)"

## test-unit: Run unit tests only
.PHONY: test-unit
test-unit:
	@echo "Running unit tests..."
	$(GOTEST) -v -race ./internal/... ./pkg/...

## test-integration: Run integration tests
.PHONY: test-integration
test-integration:
	@echo "Running integration tests..."
	$(GOTEST) -v -race ./tests/integration/...

## test-e2e: Run end-to-end tests
.PHONY: test-e2e
test-e2e:
	@echo "Running end-to-end tests..."
	$(GOTEST) -v -race ./tests/e2e/...

## lint: Run linter
.PHONY: lint
lint:
	@echo "Running linter..."
	$(GOLINT) run --timeout=5m ./...

## lint-fix: Fix linting issues automatically
.PHONY: lint-fix
lint-fix:
	@echo "Fixing linting issues..."
	$(GOLINT) run --fix ./...

## fmt: Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

## fmt-check: Check code formatting
.PHONY: fmt-check
fmt-check:
	@echo "Checking code formatting..."
	@test -z $$($(GOFMT) -l .)

## vet: Run go vet
.PHONY: vet
vet:
	@echo "Running go vet..."
	$(GOCMD) vet ./...

## check: Run all quality checks
.PHONY: check
check: fmt-check vet lint test

## docker: Build Docker images
.PHONY: docker
docker:
	@echo "Building Docker images..."
	docker build -t opsagent:latest -f deployments/docker/Dockerfile .

## docker-push: Push Docker images
.PHONY: docker-push
docker-push:
	@echo "Pushing Docker images..."
	docker push opsagent:latest

## run: Run the agent service
.PHONY: run
run: build-agent
	@echo "Running agent service..."
	$(BUILD_DIR)/$(BINARY_NAME)-agent

## run-api: Run the API service
.PHONY: run-api
run-api: build-api
	@echo "Running API service..."
	$(BUILD_DIR)/$(BINARY_NAME)-api

## install: Install binaries to GOPATH/bin
.PHONY: install
install: build
	@echo "Installing binaries..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(GOPATH)/bin/
	@cp $(BUILD_DIR)/$(BINARY_NAME)-agent $(GOPATH)/bin/
	@cp $(BUILD_DIR)/$(BINARY_NAME)-api $(GOPATH)/bin/
	@cp $(BUILD_DIR)/$(BINARY_NAME)-bot $(GOPATH)/bin/

## help: Show this help message
.PHONY: help
help:
	@echo "OpsAgent Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'

.DEFAULT_GOAL := help
