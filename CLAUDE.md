# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Additional Instructions
- commit guidelines @docs/git-instructions.md

## Development Commands

### Core Development
- `make dev` - Start both frontend and backend with hot reload
- `make dev-backend` - Backend only with air hot reload
- `make dev-frontend` - Frontend only (Vite dev server)
- `make build` - Build complete application (frontend + backend binary)

### Testing and Quality
- `make test` - Run Go tests
- `make test-openapi` - Validate OpenAPI specification  
- `make lint` - Lint both Go and TypeScript code
- `make fmt` - Format both Go and TypeScript code
- `golangci-lint run` - Go specific linting
- `cd web && pnpm lint` - Frontend linting
- `cd web && pnpm format` - Frontend formatting

### Binary Operations
- `./qui serve` - Run the server
- `./qui generate-config` - Generate default config.toml
- `./qui create-user --username admin` - Create initial user account
- `./qui update` - Self-update the binary

## Architecture Overview

### High-Level Structure
qui is a modern Go web application providing a qBittorrent WebUI alternative. The architecture follows clean architecture principles with clear separation between layers.

**Core Components:**
- **Frontend**: React/TypeScript SPA in `web/` using Vite, TanStack Router, shadcn/ui
- **Backend**: Go HTTP server using chi router with embedded frontend assets
- **Database**: SQLite with migration system
- **qBittorrent Integration**: Client pool for managing multiple instances
- **Real-time Sync**: WebSocket-like sync manager for live torrent updates

### Package Structure
```
cmd/qui/           - CLI commands and main application entry
internal/
  api/             - HTTP handlers and routing
  auth/            - Authentication and session management  
  config/          - Configuration management
  database/        - SQLite setup and migrations
  models/          - Data models and database stores
  metrics/         - Prometheus metrics collection
  polar/           - Premium theme licensing (Polar.sh integration)
  proxy/           - Reverse proxy for external app integration
  qbittorrent/     - qBittorrent client pool and sync management
  services/        - Business logic layer
  update/          - Self-update functionality
  web/             - Embedded frontend assets and handlers
web/               - React frontend source code
```

### Key Patterns

**Client Pool Architecture**: Uses a connection pool (`internal/qbittorrent/pool.go`) to manage multiple qBittorrent instances with automatic reconnection and health monitoring.

**Sync Manager**: Real-time synchronization system (`internal/qbittorrent/sync_manager.go`) that efficiently handles large torrent collections (10k+) using WebSocket-like patterns.

**Store Pattern**: Database access through dedicated stores (`internal/models/`) with proper error handling and encryption for sensitive data.

**Reverse Proxy**: Built-in proxy (`internal/proxy/`) allows external applications (Sonarr, Radarr, etc.) to access qBittorrent instances through qui without credential exposure.

## Technology Stack

### Backend
- **Language**: Go 1.24+
- **Router**: chi/v5 
- **Database**: SQLite with modernc.org/sqlite driver
- **Auth**: Session-based with Argon2 password hashing
- **Metrics**: Prometheus client
- **Self-update**: creativeprojects/go-selfupdate

### Frontend  
- **Framework**: React 19 with TypeScript
- **Router**: TanStack Router
- **State**: TanStack Query for server state
- **UI**: shadcn/ui components with Radix primitives
- **Styling**: Tailwind CSS v4
- **Build**: Vite with PWA plugin

### Notable Dependencies
- `autobrr/go-qbittorrent` - qBittorrent API client
- `gorilla/sessions` - Session management
- `spf13/viper` - Configuration
- `rs/zerolog` - Structured logging

## Development Workflow

1. **Frontend Development**: Run `make dev` for full stack or `make dev-frontend` for frontend-only development
2. **Backend Development**: Use `make dev-backend` with air for hot reload on Go changes  
3. **Testing**: Always run `make test` and `make lint` before commits
4. **Building**: Use `make build` to create production binary with embedded frontend

## Configuration

Configuration uses TOML format (`config.toml`) with environment variable overrides (`QUI__*` prefix). Key settings include database path, server host/port, base URL for reverse proxy setups, and metrics enablement.