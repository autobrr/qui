# qBittorrent WebUI Makefile

# Load .env file if it exists (silently)
ifneq (,$(wildcard .env))
    include .env
    export
endif

# Variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY_NAME = qui
BUILD_DIR = build
WEB_DIR = web
INTERNAL_WEB_DIR = internal/web

# Go build flags with Polar credentials
LDFLAGS = -ldflags "-X main.Version=$(VERSION) -X main.PolarOrgID=$(POLAR_ORG_ID)"

.PHONY: all build frontend backend dev dev-backend dev-frontend dev-expose clean test help

# Default target
all: build

# Build both frontend and backend
build: frontend backend

# Build frontend
frontend:
	@echo "Building frontend..."
	cd $(WEB_DIR) && pnpm install && pnpm build
	@echo "Copying frontend assets..."
	rm -rf $(INTERNAL_WEB_DIR)/dist
	cp -r $(WEB_DIR)/dist $(INTERNAL_WEB_DIR)/
	echo "" > $(WEB_DIR)/dist/.gitkeep

# Build backend
backend:
	@echo "Building backend..."
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/server

# Development mode - run both frontend and backend
dev:
	@echo "Starting development mode..."
	@make -j 2 dev-backend dev-frontend

# Run backend with hot reload (requires air)
dev-backend:
	@echo "Starting backend development server..."
	air -c .air.toml

# Run frontend development server
dev-frontend:
	@echo "Starting frontend development server..."
	cd $(WEB_DIR) && pnpm dev

# Development mode with frontend exposed on 0.0.0.0
dev-expose:
	@echo "Starting development mode with frontend exposed on 0.0.0.0..."
	@make -j 2 dev-backend dev-frontend-expose

# Run frontend development server exposed on 0.0.0.0
dev-frontend-expose:
	@echo "Starting frontend development server (exposed on 0.0.0.0)..."
	cd $(WEB_DIR) && pnpm dev --host

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf $(WEB_DIR)/dist $(INTERNAL_WEB_DIR)/dist $(BINARY_NAME) $(BUILD_DIR)

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Validate OpenAPI specification
test-openapi:
	@echo "Validating OpenAPI specification..."
	go test -v ./internal/web/swagger

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...
	cd $(WEB_DIR) && pnpm format

# Lint code
lint:
	@echo "Linting code..."
	golangci-lint run
	cd $(WEB_DIR) && pnpm lint

# Install development dependencies
deps:
	@echo "Installing development dependencies..."
	go mod download
	cd $(WEB_DIR) && pnpm install

# Help
help:
	@echo "Available targets:"
	@echo "  make build        - Build both frontend and backend"
	@echo "  make frontend     - Build frontend only"
	@echo "  make backend      - Build backend only"
	@echo "  make dev          - Run development servers"
	@echo "  make dev-backend  - Run backend with hot reload"
	@echo "  make dev-frontend - Run frontend development server"
	@echo "  make dev-expose   - Run frontend dev server exposed on 0.0.0.0"
	@echo "  make clean        - Clean build artifacts"
	@echo "  make test         - Run all tests"
	@echo "  make test-openapi - Validate OpenAPI specification"
	@echo "  make fmt          - Format code"
	@echo "  make lint         - Lint code"
	@echo "  make deps         - Install dependencies"
	@echo "  make help         - Show this help message"