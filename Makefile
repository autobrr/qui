# qBittorrent WebUI Makefile

# Load .env file if it exists (silently)
ifneq (,$(wildcard .env))
    include .env
    export
endif

# Variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse HEAD 2> /dev/null)
GIT_TAG := $(shell git describe --abbrev=0 --tags)
BINARY_NAME = qui
BUILD_DIR = build
WEB_DIR = web
INTERNAL_WEB_DIR = internal/web

# Go build flags with Polar credentials
LDFLAGS = -ldflags "-X github.com/autobrr/qui/internal/buildinfo.Version=$(VERSION) -X main.PolarOrgID=$(POLAR_ORG_ID)"

.PHONY: all build frontend backend dev dev-backend dev-frontend dev-expose clean test help themes-fetch themes-clean

# Default target
all: build

# Build both frontend and backend
build: frontend backend

build/docker:
	@echo "Building docker image..."
	docker build -t ghcr.io/autobrr/qui:dev -f Dockerfile . --build-arg  GIT_TAG=$(GIT_TAG) --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg POLAR_ORG_ID=$(POLAR_ORG_ID) --build-arg VERSION=$(VERSION)

build/dockerx:
	docker buildx build -t ghcr.io/autobrr/qui:dev -f Dockerfile . --build-arg GIT_TAG=$(GIT_TAG) --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg VERSION=$(VERSION) --platform=linux/amd64,linux/arm64 --pull --load

# Fetch premium themes from private repository
themes-fetch:
	@echo "Fetching premium themes..."
	@if [ -n "$$THEMES_REPO_TOKEN" ]; then \
		rm -rf .themes-temp && \
		git clone --depth=1 --filter=blob:none --sparse \
			https://$$THEMES_REPO_TOKEN@github.com/autobrr/qui-premium-themes.git .themes-temp && \
		cd .themes-temp && git sparse-checkout set --cone themes && cd .. && \
		mkdir -p $(WEB_DIR)/src/themes/premium && \
		cp .themes-temp/themes/*.css $(WEB_DIR)/src/themes/premium/ && \
		rm -rf .themes-temp && \
		echo "Premium themes fetched successfully"; \
	else \
		echo "THEMES_REPO_TOKEN not set, skipping premium themes"; \
	fi

# Clean premium themes
themes-clean:
	@echo "Cleaning premium themes..."
	rm -rf $(WEB_DIR)/src/themes/premium

# Build frontend
frontend: themes-fetch
	@echo "Building frontend..."
	cd $(WEB_DIR) && pnpm install && pnpm build
	@echo "Copying frontend assets..."
	rm -rf $(INTERNAL_WEB_DIR)/dist
	cp -r $(WEB_DIR)/dist $(INTERNAL_WEB_DIR)/

# Build backend
backend:
	@echo "Building backend..."
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/qui

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
clean: themes-clean
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

# Modernize Go code (interface{} -> any, etc)
modern:
	@echo "Modernizing Go code..."
	go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix -test ./...

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
	@echo "  make themes-fetch - Fetch premium themes from private repository"
	@echo "  make themes-clean - Clean premium themes"
	@echo "  make dev          - Run development servers"
	@echo "  make dev-backend  - Run backend with hot reload"
	@echo "  make dev-frontend - Run frontend development server"
	@echo "  make dev-expose   - Run frontend dev server exposed on 0.0.0.0"
	@echo "  make clean        - Clean build artifacts"
	@echo "  make test         - Run all tests"
	@echo "  make test-openapi - Validate OpenAPI specification"
	@echo "  make fmt          - Format code"
	@echo "  make lint         - Lint code"
	@echo "  make modern       - Modernize Go code (interface{} -> any, etc)"
	@echo "  make deps         - Install dependencies"
	@echo "  make help         - Show this help message"