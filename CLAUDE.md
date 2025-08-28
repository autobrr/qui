# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

qui - a self-hosted, single-user web interface for managing multiple qBittorrent instances. Built with Go backend and React frontend.

**Important**: See `prd_final.md` for the complete implementation plan, architecture details, and technical specifications.

## Development Commands

### Backend (Go)
```bash
# Install dependencies
go mod download

# Build backend
go build -ldflags "-X main.Version=$(git describe --tags --always)" -o qui ./cmd/qui

# Run backend in development
air -c .air.toml  # Hot reload

# Run tests
go test ./...

# Run specific test
go test -v -run TestFunctionName ./path/to/package

# Run integration tests (requires qBittorrent instance)
go test -v -tags=integration ./internal/qbittorrent

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run with race detection
go test -race ./...

# Benchmark tests
go test -bench=. ./internal/qbittorrent

# Validate OpenAPI spec
go test -v ./internal/web/swagger
```

### Frontend (React/TypeScript)
```bash
# Install dependencies
cd web && pnpm install

# Run frontend in development
cd web && pnpm dev

# Build frontend for production
cd web && pnpm build

# Lint frontend code
cd web && pnpm lint

# Format frontend code
cd web && pnpm format

# Type check
cd web && pnpm tsc -b
```

### Full Build
```bash
# Build both frontend and backend into single binary
make build

# Development mode (run both frontend and backend)
make dev

# Run backend with hot reload
make dev-backend

# Run frontend development server
make dev-frontend

# Format code
make fmt

# Lint code
make lint

# Modernize Go code (interface{} -> any, etc)
make modern

# Validate OpenAPI specification
make test-openapi

# Install all dependencies
make deps

# Clean build artifacts
make clean
```

## Architecture

### Backend Architecture
- **Framework**: Chi router (github.com/go-chi/chi/v5)
- **Database**: SQLite via modernc.org/sqlite (CGO-free)
- **qBittorrent Client**: github.com/autobrr/go-qbittorrent
- **Session Management**: gorilla/sessions with secure cookies
- **Configuration**: Viper with TOML config and environment overrides
- **Performance**: Ristretto cache, ants/v2 goroutine pool
- **Logging**: zerolog with configurable levels

Key patterns:
- Frontend assets embedded using `go:embed` 
- Single user authentication (no multi-tenancy)
- Connection pooling for multiple qBittorrent instances
- Cache-first architecture with coordinated invalidation

### Frontend Architecture
- **Build**: Vite with React 19 and TypeScript
- **Routing**: TanStack Router for type-safe routes
- **Data**: TanStack Query for server state, TanStack Form for forms
- **UI**: shadcn/ui components exclusively (no custom modifications)
- **Tables**: TanStack Table v8 with TanStack Virtual for performance
- **Styling**: Tailwind CSS v4 with CSS-first configuration
- **State**: React hooks + TanStack Query (no global state library)

#### Important: shadcn/ui Component Installation
The project is already configured for shadcn/ui components:
- **Location**: Components are installed in `web/src/components/ui/`
- **Configuration**: `components.json` is configured to use `src/components/ui`
- **Installation**: Use `cd web && pnpm dlx shadcn@latest add <component-name>`

Key patterns:
- Server-side operations for large datasets
- Virtual scrolling for performance
- Incremental sync updates via SyncMainData
- Progressive loading for initial render
- Optimistic UI updates with delayed server invalidation
- PWA support with service worker (production builds only)
- Theme system with CSS-based configuration

#### TanStack Form Best Practices
- **Avoid `form.reset()`**: Use individual `form.setFieldValue(field, value)` calls for updating form values
- **Use nullish coalescing (`??`) not logical OR (`||`)**: Prevents `0`, `false`, and `""` from being replaced with fallbacks
- **Example**:
  ```typescript
  // ❌ Problematic - reset() doesn't work reliably, || treats 0 as falsy
  form.reset({ 
    max_downloads: preferences.max_downloads || 3 
  })
  
  // ✅ Correct - setFieldValue works reliably, ?? only fallbacks null/undefined
  form.setFieldValue("max_downloads", preferences.max_downloads ?? 3)
  ```

## Configuration

Environment variables use `qui__` prefix:
- `qui__HOST` (default: localhost or 0.0.0.0 in containers)
- `qui__PORT` (default: 7476)
- `qui__BASE_URL` (serve app under subdirectory, e.g., "/qui/")
- `qui__SESSION_SECRET` (auto-generated if not set)
- `qui__LOG_LEVEL` (ERROR, DEBUG, INFO, WARN, TRACE)

Config file: `config.toml` (auto-created on first run)
Database file: `qui.db` (always created next to config.toml)

## API Endpoints

Protected routes require authentication via session cookie or API key header (`X-API-Key`).

- `POST /api/auth/setup` - Initial user setup
- `POST /api/auth/login` - User login
- `GET /api/instances` - List all instances
- `POST /api/instances` - Add new instance
- `GET /api/instances/{id}/torrents` - Get torrents (paginated)
- `GET /api/instances/{id}/torrents/sync` - SyncMainData endpoint
- `POST /api/instances/{id}/torrents` - Add torrent
- `POST /api/instances/{id}/torrents/bulk-action` - Bulk operations
- `GET /metrics` - Prometheus metrics endpoint (requires API key)

## Database Schema

Single-user design with encrypted instance credentials:
- `user` table - Single record enforced
- `api_keys` table - For automation/scripts
- `instances` table - qBittorrent instance connections

## Testing Strategy

- Backend: Table-driven tests for handlers
- Frontend: React Testing Library for components
- E2E: Playwright for critical user flows

### Running Tests

```bash
# Run all backend tests
go test ./...

# Run specific package tests
go test -v ./internal/qbittorrent

# Run integration tests
go test -v -tags=integration ./internal/qbittorrent

# Run with race detection
go test -race ./...

# Benchmark cache performance
go test -bench=. ./internal/qbittorrent
```

## Commit Guidelines

Follow Conventional Commit format with package name as scope:
- `feat(package):` New feature
- `fix(package):` Bug fix
- `docs(package):` Documentation only
- `style(package):` Code style changes
- `refactor(package):` Code refactoring
- `test(package):` Test changes
- `chore(package):` Build process or auxiliary tool changes

Examples using Go package names as scopes:
- `feat(metrics):` Add Prometheus metrics endpoint
- `fix(qbittorrent):` Correct connection pooling
- `refactor(api):` Simplify handler structure
- `test(database):` Add migration tests
- `chore(deps):` Update Go dependencies

For frontend changes, use:
- `feat(web):` Frontend changes
- `fix(web):` Frontend bug fixes

**CRITICAL**: Never reference Claude or Claude Code in any commit messages

## Commit Co-Authors

- **Important**: Never add co-authors to commits at all

## Common Development Workflows

### Adding a New API Endpoint
1. Define handler in `internal/api/handlers/`
2. Add route in `internal/api/router.go`
3. Update frontend API client in `web/src/lib/api.ts`
4. Add TypeScript types in `web/src/types/index.ts`
5. Create React Query hook in `web/src/hooks/`

### Adding a New shadcn/ui Component
```bash
cd web
pnpm dlx shadcn@latest add <component-name>
# Components are installed in web/src/components/ui/
# The project is pre-configured with components.json
```

### Debugging Performance Issues
1. Check backend logs with `LOG_LEVEL=DEBUG`
2. Monitor cache hit rates in sync_manager.go
3. Use browser DevTools Performance tab for frontend
4. Check virtual scrolling in TorrentTableOptimized.tsx

## Key Implementation Details

### Backend
- **Database Migrations**: Auto-run on startup via `internal/database/migrations.go`
- **Instance Connections**: Pooled and health-checked every 30 seconds
- **Password Storage**: Argon2id hashing with secure defaults
- **Session Management**: HTTP-only cookies with CSRF protection
- **API Response Format**: Always camelCase JSON (handled by converters)

### Frontend
- **Route Guards**: Authentication checked in `_authenticated.tsx` layout
- **Real-time Updates**: 
  - Dashboard uses 10-second polling for stats
  - Torrent table uses paginated API with 5-second polling
- **State Management**: 
  - Server state: TanStack Query with 5s stale time
  - UI state: React useState/useReducer
- **Virtual Scrolling**: Progressive loading starting at 100 rows
- **Column Resizing**: Persisted in component state (not localStorage)
- **Torrent Details**: 
  - Click torrent row to view details panel below table
  - Tabs for General, Trackers, and Content (Files)
  - Responsive split layout (side-by-side on desktop, stacked on mobile)

### Critical Files
- `internal/qbittorrent/sync_manager.go` - Handles all torrent operations and caching
- `web/src/hooks/useTorrentsList.ts` - Main hook for torrent data fetching
- `web/src/components/torrents/TorrentTableOptimized.tsx` - Virtual scrolling implementation with torrent details
- `web/src/components/torrents/TorrentDetailsPanel.tsx` - Torrent details panel with tabs
- `internal/api/handlers/torrents.go` - Backend filtering and pagination logic

## Docker Deployment

The application can be deployed using Docker:

```bash
# Using Docker Compose (recommended)
docker compose up -d

# Or standalone
docker run -d \
  -v $(pwd)/config:/config \
  ghcr.io/autobrr/qui:latest
```

### Environment Variables for Docker
- `QUI__HOST`: Listen address (default: 0.0.0.0 in containers)
- `QUI__PORT`: Port number (default: 8080)
- `QUI__BASE_URL`: Serve from subdirectory (e.g., "/qui/")
- `QUI__SESSION_SECRET`: Cookie encryption secret
- `QUI__LOG_LEVEL`: Logging level (ERROR, DEBUG, INFO, WARN, TRACE)

## Base URL Configuration

When serving the application from a subdirectory (e.g., `/qui/`), set the `baseUrl` in config:

```toml
baseUrl = "/qui/"
```

This configuration:
- Mounts all routes under the specified path
- Injects the base URL into the frontend at runtime (no rebuild needed)
- Updates API endpoints and router navigation automatically
- Redirects root path `/` to the base URL

Example nginx proxy configuration:
```nginx
location /qui/ {
    proxy_pass http://localhost:7476/qui/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
}
```

## Theme System

### Theme Configuration
- **Theme Files**: Located in `web/src/themes/`
- **Theme Selection**: Available in Settings → Appearance
- **License Validation**: Some themes require license validation
- **Custom Themes**: CSS-based theme system with CSS variables

### Available Themes
Multiple built-in themes including minimal, cyberpunk, catppuccin, and more.

## PWA Configuration

### Progressive Web App Features
- **Service Worker**: Enabled in production builds only
- **Caching Strategy**: 
  - NetworkFirst for API calls (5-minute cache)
  - CacheFirst for fonts (1-year cache)
- **Offline Support**: Basic offline functionality with cached resources
- **Installation**: Can be installed as a native app on supported devices

## Known Issues and Workarounds
- SyncMainData implementation exists but is currently unused due to complexity
- Tags can be either string[] or comma-separated string from API
- Instance status requires periodic health checks due to connection drops
- **TanStack Form `form.reset()` Issue**: `form.reset()` does not reliably update form field values. Use individual `form.setFieldValue(field, value)` calls instead

## Cache Management and Real-time Updates

### Backend Cache Strategy
The backend uses Ristretto cache with **2-second TTL** for torrent data to ensure responsive updates after user actions. Key considerations:

- **Cache Invalidation**: Immediately clear cache after bulk actions (pause/resume/delete) and adding torrents
- **qBittorrent Processing Time**: qBittorrent needs time to process actions before its API reflects changes
- **Cache TTL**: Reduced from 10-30 seconds to 2 seconds for better responsiveness

### Frontend Update Strategy
Frontend uses coordinated timing with backend for optimal user experience:

- **TorrentActions**: 1-second delay before React Query invalidation
- **AddTorrentDialog**: 500ms delay before React Query invalidation  
- **React Query**: 5-second stale time with `exact: false` invalidation to match all related queries

### Implementation Pattern
```go
// Backend: Immediate cache clear after action
h.syncManager.BulkAction(ctx, instanceID, hashes, action)
h.syncManager.InvalidateCache(instanceID) // Clear entire cache

// Frontend: Delayed invalidation to allow qBittorrent processing
setTimeout(() => {
  queryClient.invalidateQueries({ 
    queryKey: ['torrents-list', instanceId],
    exact: false 
  })
}, 1000) // 1 second for actions, 500ms for adding torrents
```

This approach ensures:
1. Actions return immediately (good UX)  
2. Backend cache is cleared immediately
3. Frontend waits for qBittorrent to process changes
4. Next API call gets fresh data with updated torrent states

## Cache Architecture and qBittorrent Protection

### Cache Implementation
The backend uses **Ristretto** high-performance cache to prevent overwhelming qBittorrent instances:

- **Cache Capacity**: 1GB memory, 10M counters
- **Shared Cache**: Single cache serves all qBittorrent instances
- **Metrics Available**: Hit/miss ratios exposed for monitoring via pool stats

### Cache TTL Strategy
Different data types have optimized TTL values:

- **Torrent Lists**: 2-second TTL (main torrent data for responsiveness)
- **Categories/Tags**: 60-second TTL (metadata rarely changes)  
- **Torrent Properties**: 30-second TTL (individual torrent details)
- **Filtered Results**: 5-second TTL (search/filter combinations)
- **Torrent Files/Trackers**: 30-second TTL (detailed torrent data)

### Cache Key Structure
Every API endpoint uses specific cache keys to prevent collisions:
```
torrents:filtered:{instanceId}:{offset}:{limit}:{sort}:{order}:{search}:{filters}
categories:{instanceId}
tags:{instanceId}
torrent:properties:{instanceId}:{hash}
torrent:trackers:{instanceId}:{hash}
```

### Protection Mechanisms
1. **Respectful Polling**: Frontend React Query polls every 5 seconds (not aggressive)
2. **Cache-First**: Backend checks cache before qBittorrent API calls
3. **Coordinated Invalidation**: Cache cleared immediately after user actions
4. **Batch Operations**: Multiple requests coalesced where possible
5. **Health Monitoring**: Instance connections health-checked every 30 seconds

### Monitoring Cache Performance
Cache metrics are exposed in the client pool stats:
- `cache_hits`: Number of successful cache lookups
- `cache_misses`: Number of cache misses requiring qBittorrent API calls
- Monitor these to ensure qBittorrent instances aren't being overwhelmed

## Prometheus Metrics

The `/metrics` endpoint exposes Prometheus metrics for monitoring. Implementation details:

### Metrics Architecture
- **Custom Collector**: Implements `prometheus.Collector` interface in `internal/metrics/collector.go`
- **Proactive Connection**: Establishes connections when scraped (no UI required)
- **On-demand Calculation**: Metrics computed during scrape, not pre-stored
- **API Key Required**: Uses existing API key authentication system

### Available Metrics
- `qbittorrent_torrents_*` - Torrent counts by status (downloading, seeding, paused, error, checking)
- `qbittorrent_*_speed_bytes_per_second` - Upload/download speeds
- `qbittorrent_instance_connection_status` - Instance health (1=connected, 0=disconnected)

All metrics labeled with `instance_id` and `instance_name` for multi-instance monitoring.