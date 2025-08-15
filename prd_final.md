# qBittorrent Alternative WebUI - Final Implementation Plan

## Executive Summary

This document outlines the implementation plan for a **self-hosted, single-user** qBittorrent alternative web interface designed for personal use. The application supports multiple qBittorrent instances and handles large-scale deployments (10k+ torrents) while maintaining simplicity and ease of deployment. The solution will be distributed as a single binary with embedded frontend assets, making it ideal for homelab and personal server deployments.

**Key Design Principles:**
- **Self-Hosted**: Designed to run on personal servers, NAS devices, or homelabs
- **Single User**: No multi-tenancy, user management, or complex permissions
- **Privacy-First**: All data stays on your own infrastructure
- **Simple Deployment**: Single binary with no external dependencies

## Architecture Overview

### Backend Architecture (Go)
- **Framework**: Chi router with middleware chain
- **Database**: SQLite (modernc.org/sqlite - CGO-free)
- **Authentication**: Session-based auth with cookies (gorilla/sessions)
- **qBittorrent Integration**: autobrr/go-qbittorrent client library
- **Configuration**: Viper for environment and file-based config
- **CLI**: Cobra for command-line interface
- **Frontend Embedding**: Go embed package following autobrr's pattern

### Frontend Architecture (React/TypeScript)
- **Build Tool**: Vite with @tailwindcss/vite plugin
- **Framework**: React 19 with TypeScript (using new JSX transform)
- **Styling**: Tailwind CSS v4 (CSS-first configuration)
- **Routing**: TanStack Router for type-safe routing
- **Data Management**: TanStack Query for server state
- **Form Management**: TanStack Form for type-safe forms with validation
- **UI Components**: shadcn/ui exclusively (no custom modifications)
- **Table**: TanStack Table v8 with TanStack Virtual for performance

## Implementation Phases

### Phase 1: Project Setup and Infrastructure (Days 1-3)

#### Backend Setup
```bash
# Initialize Go module
go mod init github.com/autobrr/qui

# Install core dependencies
go get -u github.com/go-chi/chi/v5
go get -u github.com/go-chi/chi/v5/middleware
go get -u github.com/spf13/cobra
go get -u github.com/spf13/viper
go get -u modernc.org/sqlite
go get -u github.com/autobrr/go-qbittorrent
go get -u github.com/gorilla/sessions
go get -u golang.org/x/crypto
go get -u github.com/rs/zerolog

# Performance-critical dependencies
go get -u github.com/dgraph-io/ristretto    # High-performance cache
go get -u github.com/panjf2000/ants/v2      # Goroutine pool
go get -u github.com/fsnotify/fsnotify      # Config hot-reload

# Run go mod tidy
go mod tidy
```

#### Frontend Setup
```bash
# Create Vite project with React 19
pnpm create vite@latest web --template react-ts
cd web

# Install React 19
pnpm add react@^19.0.0 react-dom@^19.0.0

# Install core dependencies
pnpm add @tanstack/react-router@latest
pnpm add @tanstack/react-query@latest
pnpm add @tanstack/react-table@latest
pnpm add @tanstack/react-virtual@latest
pnpm add @tanstack/react-form@latest
pnpm add -D tailwindcss@latest @tailwindcss/vite@latest
pnpm add -D @types/react@latest @types/react-dom@latest

# Initialize shadcn/ui
pnpm dlx shadcn@latest init
```

#### Project Structure
```
qui/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── api/
│   │   ├── handlers/
│   │   │   ├── auth.go
│   │   │   ├── instances.go
│   │   │   └── torrents.go
│   │   ├── middleware/
│   │   │   ├── auth.go
│   │   │   └── cors.go
│   │   └── router.go
│   ├── auth/
│   │   ├── service.go
│   │   └── argon2.go
│   ├── config/
│   │   └── config.go
│   ├── database/
│   │   ├── db.go
│   │   └── migrations/
│   ├── models/
│   │   ├── instance.go
│   │   ├── user.go
│   │   └── api_key.go
│   ├── qbittorrent/
│   │   ├── client.go
│   │   └── pool.go
│   └── web/
│       └── handler.go
├── web/
│   ├── src/
│   │   ├── components/
│   │   │   ├── torrents/
│   │   │   │   ├── TorrentTable.tsx
│   │   │   │   ├── TorrentActions.tsx
│   │   │   │   └── AddTorrentDialog.tsx
│   │   │   ├── layout/
│   │   │   │   ├── Sidebar.tsx
│   │   │   │   └── Header.tsx
│   │   │   └── ui/ (shadcn components)
│   │   ├── hooks/
│   │   │   ├── useInstances.ts
│   │   │   └── useTorrents.ts
│   │   ├── lib/
│   │   │   ├── api.ts
│   │   │   └── utils.ts
│   │   ├── pages/
│   │   │   ├── Login.tsx
│   │   │   ├── Dashboard.tsx
│   │   │   └── Settings.tsx
│   │   ├── routes/
│   │   │   └── index.tsx
│   │   └── types/
│   │       └── index.ts
│   ├── index.html
│   └── vite.config.ts
├── Makefile
├── go.mod
├── go.sum
├── package.json
├── pnpm-lock.yaml
└── README.md
```

### Phase 2: Core Backend Implementation (Days 4-7)

#### 1. Database Schema and Models
```sql
-- Single user table (only one record)
CREATE TABLE user (
    id INTEGER PRIMARY KEY CHECK (id = 1), -- Ensures only one user
    username TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- api_keys table (for automation/scripts)
CREATE TABLE api_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key_hash TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP
);

-- instances table
CREATE TABLE instances (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    username TEXT NOT NULL,
    password_encrypted TEXT NOT NULL,
    is_active BOOLEAN DEFAULT 1,
    last_connected_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Performance indexes for 10k+ torrents
CREATE INDEX idx_instances_active ON instances(is_active);
CREATE INDEX idx_api_keys_hash ON api_keys(key_hash);
```

#### 2. Configuration System (Viper)
```go
// internal/config/config.go
package config

import (
    "github.com/spf13/viper"
    "github.com/autobrr/qui/internal/domain"
)

type AppConfig struct {
    Config *domain.Config
    viper  *viper.Viper
}

func New(configPath string) (*AppConfig, error) {
    c := &AppConfig{
        viper: viper.New(),
    }
    
    // Set defaults
    c.defaults()
    
    // Load from config file
    if err := c.load(configPath); err != nil {
        return nil, err
    }
    
    // Override with environment variables
    c.loadFromEnv()
    
    return c, nil
}

func (c *AppConfig) load(configPath string) error {
    c.viper.SetConfigType("toml")
    
    if configPath != "" {
        // Create default config if doesn't exist
        if err := c.writeConfig(configPath); err != nil {
            return err
        }
        c.viper.SetConfigFile(filepath.Join(configPath, "config.toml"))
    } else {
        c.viper.SetConfigName("config")
        c.viper.AddConfigPath(".")
        c.viper.AddConfigPath("$HOME/.config/qui")
        c.viper.AddConfigPath("$HOME/.qui")
    }
    
    if err := c.viper.ReadInConfig(); err != nil {
        return err
    }
    
    return c.viper.Unmarshal(c.Config)
}
```

#### 3. Authentication System (Single-User)
- Simple authentication for a single user (self-hosted use case)
- Cookie-based session management using gorilla/sessions
- Secure password hashing using argon2id
- HttpOnly, Secure, SameSite cookies for CSRF protection
- Initial setup creates the single user account
- API key support for automation and programmatic access

##### Session Management Implementation
```go
// internal/http/auth.go
import "github.com/gorilla/sessions"

var store = sessions.NewCookieStore([]byte(sessionSecret))

func (h *authHandler) login(w http.ResponseWriter, r *http.Request) {
    // Validate credentials
    user, err := h.authService.Login(username, password)
    if err != nil {
        // Handle error
    }
    
    // Create session
    session, _ := store.Get(r, "user_session")
    session.Values["authenticated"] = true
    session.Values["user_id"] = user.ID
    session.Values["username"] = user.Username
    
    // Configure cookie security
    session.Options = &sessions.Options{
        Path:     "/",
        MaxAge:   86400 * 7, // 7 days
        HttpOnly: true,
        Secure:   r.TLS != nil,
        SameSite: http.SameSiteLaxMode,
    }
    
    session.Save(r, w)
}

// Middleware
func IsAuthenticated(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Check for API key first
        if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
            // Validate API key
            if valid := validateAPIKey(apiKey); valid {
                next.ServeHTTP(w, r)
                return
            }
        }
        
        // Check session
        session, _ := store.Get(r, "user_session")
        if auth, ok := session.Values["authenticated"].(bool); !ok || !auth {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        
        next.ServeHTTP(w, r)
    })
}
```

#### 3. qBittorrent Client Pool & Sync Manager
- Connection pool for managing multiple instances
- Health checks and automatic reconnection
- Encrypted credential storage
- Request routing based on instance ID
- SyncMainData support for efficient updates with 10k+ torrents

##### SyncMainData Implementation
```go
// internal/qbittorrent/sync_manager.go
type SyncManager struct {
    clients     map[string]*qbittorrent.Client
    mainData    map[string]*qbittorrent.MainData
    ridTracker  map[string]int64
    mu          sync.RWMutex
    cache       *ristretto.Cache
}

// Initial load with pagination
func (sm *SyncManager) InitialLoad(instanceID string, limit, offset int) (*TorrentResponse, error) {
    client := sm.getClient(instanceID)
    
    // Use GetTorrentsCtx for initial paginated load
    opts := qbittorrent.TorrentFilterOptions{
        Limit:  &limit,
        Offset: &offset,
        Sort:   "added_on",
        Reverse: true,
    }
    
    torrents, err := client.GetTorrentsCtx(ctx, opts)
    if err != nil {
        return nil, err
    }
    
    // Cache the initial data
    sm.cache.Set(fmt.Sprintf("torrents:%s:%d:%d", instanceID, offset, limit), torrents, 1)
    
    return &TorrentResponse{
        Torrents: torrents,
        Total:    sm.getTotalCount(instanceID),
    }, nil
}

// Real-time updates using SyncMainData
func (sm *SyncManager) GetUpdates(instanceID string) (*qbittorrent.MainData, error) {
    sm.mu.Lock()
    rid := sm.ridTracker[instanceID]
    sm.mu.Unlock()
    
    client := sm.getClient(instanceID)
    mainData, err := client.SyncMainDataCtx(ctx, rid)
    if err != nil {
        return nil, err
    }
    
    // Update RID for next request
    sm.mu.Lock()
    sm.ridTracker[instanceID] = mainData.Rid
    
    // Merge updates into existing data
    if existing, ok := sm.mainData[instanceID]; ok && !mainData.FullUpdate {
        existing.Update(mainData)
        sm.mainData[instanceID] = existing
    } else {
        sm.mainData[instanceID] = mainData
    }
    sm.mu.Unlock()
    
    return mainData, nil
}
```

#### 4. API Implementation
Following RESTful principles with Chi router:

```go
// Router setup
r := chi.NewRouter()
r.Use(middleware.Logger)
r.Use(middleware.Recoverer)
r.Use(middleware.Cors())

// API routes
r.Route("/api", func(r chi.Router) {
    // Public routes (no registration - single user only)
    r.Post("/auth/setup", h.Setup) // Initial setup if no user exists
    r.Post("/auth/login", h.Login)
    
    // Protected routes
    r.Group(func(r chi.Router) {
        r.Use(IsAuthenticated)
        
        // Auth
        r.Post("/auth/logout", h.Logout)
        r.Get("/auth/me", h.GetCurrentUser)
        
        // Instances
        r.Route("/instances", func(r chi.Router) {
            r.Get("/", h.ListInstances)
            r.Post("/", h.CreateInstance)
            r.Route("/{instanceID}", func(r chi.Router) {
                r.Put("/", h.UpdateInstance)
                r.Delete("/", h.DeleteInstance)
                r.Post("/test", h.TestConnection)
                
                // Torrents
                r.Get("/torrents", h.ListTorrents)
                r.Get("/torrents/sync", h.SyncTorrents) // SyncMainData endpoint
                r.Post("/torrents", h.AddTorrent)
                r.Post("/torrents/bulk-action", h.BulkAction)
                r.Route("/torrents/{hash}", func(r chi.Router) {
                    r.Delete("/", h.DeleteTorrent)
                    r.Put("/pause", h.PauseTorrent)
                    r.Put("/resume", h.ResumeTorrent)
                })
                
                // Categories/Tags
                r.Get("/categories", h.ListCategories)
                r.Get("/tags", h.ListTags)
            })
        })
    })
})
```

### Phase 3: Frontend Implementation (Days 8-12)

#### 1. Tailwind CSS v4 Setup
```css
/* src/index.css */
@import 'tailwindcss';

/* Custom theme configuration using CSS variables */
@theme {
  --color-primary: #3b82f6;
  --color-secondary: #10b981;
  --color-destructive: #ef4444;
}
```

```ts
// vite.config.ts
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [
    react({
      // React 19 requires the new JSX transform
      jsxRuntime: 'automatic',
    }),
    tailwindcss(),
  ],
})
```

#### 2. TanStack Router Setup
```tsx
// src/routes/index.tsx
import { createFileRoute, createRootRoute, createRouter } from '@tanstack/react-router'

const rootRoute = createRootRoute({
  component: RootLayout,
})

const indexRoute = createFileRoute('/')({
  component: Dashboard,
})

const loginRoute = createFileRoute('/login')({
  component: Login,
})

export const router = createRouter({
  routeTree: rootRoute.addChildren([indexRoute, loginRoute]),
})
```

#### 3. TanStack Table Implementation
```tsx
// src/components/torrents/TorrentTable.tsx
import { useVirtualizer } from '@tanstack/react-virtual'
import {
  useReactTable,
  getCoreRowModel,
  flexRender,
  ColumnDef,
  PaginationState,
  SortingState,
  ColumnFiltersState,
} from '@tanstack/react-table'
import { useState } from 'react'

const columns: ColumnDef<Torrent>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    cell: ({ row }) => (
      <div className="truncate max-w-xs" title={row.original.name}>
        {row.original.name}
      </div>
    ),
  },
  {
    accessorKey: 'progress',
    header: 'Progress',
    cell: ({ row }) => (
      <Progress value={row.original.progress * 100} className="w-20" />
    ),
  },
  // ... other columns
]

export function TorrentTable({ instanceId }: { instanceId: string }) {
  // Server-side state management for 10k+ torrents
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 50,
  })
  const [sorting, setSorting] = useState<SortingState>([])
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([])

  // Fetch data based on current state
  const { data, isLoading } = useTorrents(instanceId, {
    page: pagination.pageIndex,
    limit: pagination.pageSize,
    sorting,
    filters: columnFilters,
  })

  const table = useReactTable({
    data: data?.torrents ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
    // Server-side operations - disable client-side processing
    manualPagination: true,
    manualSorting: true,
    manualFiltering: true,
    // Provide total count for proper pagination
    rowCount: data?.totalCount ?? 0,
    // State management
    state: {
      pagination,
      sorting,
      columnFilters,
    },
    onPaginationChange: setPagination,
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
  })

  // Virtualization for current page rows
  const { rows } = table.getRowModel()
  const parentRef = React.useRef<HTMLDivElement>(null)

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 45,
    overscan: 10, // Render more rows for smoother scrolling
  })

  const virtualRows = virtualizer.getVirtualItems()

  return (
    <div className="relative h-[600px] overflow-auto" ref={parentRef}>
      <table className="w-full">
        <thead className="sticky top-0 bg-background z-10">
          {table.getHeaderGroups().map(headerGroup => (
            <tr key={headerGroup.id}>
              {headerGroup.headers.map(header => (
                <th key={header.id}>
                  {flexRender(
                    header.column.columnDef.header,
                    header.getContext()
                  )}
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody
          style={{
            height: `${virtualizer.getTotalSize()}px`,
            width: '100%',
            position: 'relative',
          }}
        >
          {virtualRows.map(virtualRow => {
            const row = rows[virtualRow.index]
            return (
              <tr
                key={row.id}
                style={{
                  position: 'absolute',
                  top: 0,
                  left: 0,
                  width: '100%',
                  height: `${virtualRow.size}px`,
                  transform: `translateY(${virtualRow.start}px)`,
                }}
              >
                {row.getVisibleCells().map(cell => (
                  <td key={cell.id}>
                    {flexRender(
                      cell.column.columnDef.cell,
                      cell.getContext()
                    )}
                  </td>
                ))}
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
```

#### 4. Data Fetching with TanStack Query
```tsx
// src/hooks/useTorrents.ts
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { SortingState, ColumnFiltersState } from '@tanstack/react-table'

interface TorrentQueryParams {
  page: number
  limit: number
  sorting: SortingState
  filters: ColumnFiltersState
}

export function useTorrents(instanceId: string, params: TorrentQueryParams) {
  return useQuery({
    queryKey: ['torrents', instanceId, params],
    queryFn: () => api.getTorrents(instanceId, {
      page: params.page,
      limit: params.limit,
      sort: params.sorting[0]?.id,
      order: params.sorting[0]?.desc ? 'desc' : 'asc',
      filters: params.filters,
    }),
    staleTime: 5000,
    refetchInterval: 5000, // Poll every 5 seconds
    placeholderData: (previousData) => previousData, // Keep previous data while fetching
  })
}

export function useTorrentActions(instanceId: string) {
  const queryClient = useQueryClient()

  const pauseTorrent = useMutation({
    mutationFn: (hash: string) => api.pauseTorrent(instanceId, hash),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['torrents', instanceId] })
    },
  })

  // Bulk operations with React 19's useOptimistic
  const bulkAction = useMutation({
    mutationFn: ({ hashes, action }: { hashes: string[], action: string }) => 
      api.bulkAction(instanceId, { hashes, action }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['torrents', instanceId] })
    },
  })

  return { pauseTorrent, bulkAction, /* ... */ }
}
```

#### 5. Form Management with TanStack Form
```tsx
// src/components/instances/InstanceForm.tsx
// Complex settings form with TanStack Form
import { useForm } from '@tanstack/react-form'
import { useMutation } from '@tanstack/react-query'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'

interface InstanceFormData {
  name: string
  host: string
  port: number
  username: string
  password: string
}

export function InstanceForm({ instance, onSuccess }: { instance?: Instance, onSuccess: () => void }) {
  const mutation = useMutation({
    mutationFn: (data: InstanceFormData) => 
      instance ? api.updateInstance(instance.id, data) : api.createInstance(data),
    onSuccess,
  })

  const form = useForm({
    defaultValues: {
      name: instance?.name ?? '',
      host: instance?.host ?? 'http://localhost',
      port: instance?.port ?? 8080,
      username: instance?.username ?? '',
      password: '',
    },
    onSubmit: async ({ value }) => {
      await mutation.mutateAsync(value)
    },
  })

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
    >
      <form.Field
        name="name"
        validators={{
          onChange: ({ value }) => 
            !value ? 'Instance name is required' : undefined,
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor={field.name}>Instance Name</Label>
            <Input
              id={field.name}
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Field
        name="host"
        validators={{
          onChange: ({ value }) => {
            if (!value) return 'Host is required'
            if (!value.match(/^https?:\/\//)) return 'Host must start with http:// or https://'
            return undefined
          },
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor={field.name}>Host URL</Label>
            <Input
              id={field.name}
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
              placeholder="http://localhost"
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Field
        name="port"
        validators={{
          onChange: ({ value }) => {
            if (!value || value < 1 || value > 65535) {
              return 'Port must be between 1 and 65535'
            }
            return undefined
          },
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor={field.name}>Port</Label>
            <Input
              id={field.name}
              type="number"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(parseInt(e.target.value))}
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Field name="username">
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor={field.name}>Username</Label>
            <Input
              id={field.name}
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
            />
          </div>
        )}
      </form.Field>

      <form.Field
        name="password"
        validators={{
          onChange: ({ value }) => 
            !instance && !value ? 'Password is required for new instances' : undefined,
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor={field.name}>Password</Label>
            <Input
              id={field.name}
              type="password"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
              placeholder={instance ? 'Leave empty to keep current password' : ''}
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <div className="flex gap-2 mt-6">
        <form.Subscribe
          selector={(state) => [state.canSubmit, state.isSubmitting]}
        >
          {([canSubmit, isSubmitting]) => (
            <Button type="submit" disabled={!canSubmit || isSubmitting}>
              {isSubmitting ? 'Saving...' : instance ? 'Update Instance' : 'Add Instance'}
            </Button>
          )}
        </form.Subscribe>
        
        <Button
          type="button"
          variant="outline"
          onClick={() => form.reset()}
        >
          Reset
        </Button>
      </div>
    </form>
  )
}

// Simpler form for adding torrents
// src/components/torrents/AddTorrentForm.tsx
import { useForm } from '@tanstack/react-form'

export function AddTorrentForm({ instanceId }: { instanceId: string }) {
  const form = useForm({
    defaultValues: {
      torrentFile: null as File | null,
      category: '',
      tags: [] as string[],
      startPaused: false,
    },
    onSubmit: async ({ value }) => {
      await api.addTorrent(instanceId, value)
      form.reset()
    },
  })

  return (
    <form onSubmit={(e) => { e.preventDefault(); form.handleSubmit() }}>
      <form.Field
        name="torrentFile"
        validators={{
          onChange: ({ value }) => !value ? 'Please select a torrent file' : undefined,
        }}
      >
        {(field) => (
          <div>
            <Input
              type="file"
              accept=".torrent"
              onChange={(e) => field.handleChange(e.target.files?.[0] || null)}
            />
            {field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      {/* Additional fields for category, tags, etc. */}
      
      <Button type="submit" disabled={!form.state.canSubmit}>
        Add Torrent
      </Button>
    </form>
  )
}

// Optimistic UI updates for better UX
export function TorrentRow({ torrent, onPause }: TorrentRowProps) {
  const [optimisticStatus, setOptimisticStatus] = useOptimistic(
    torrent.status,
    (currentStatus, newStatus: string) => newStatus
  )

  const handlePause = async () => {
    setOptimisticStatus('paused')
    await onPause(torrent.hash)
  }

  return (
    <div className={`torrent-row ${optimisticStatus}`}>
      {/* Row content */}
    </div>
  )
}

// Using React 19's use() for resource reading
import { use } from 'react'

export function InstanceSelector() {
  const instances = use(instancesPromise) // Suspends until promise resolves
  
  return (
    <select>
      {instances.map(instance => (
        <option key={instance.id} value={instance.id}>
          {instance.name}
        </option>
      ))}
    </select>
  )
}
```

### Phase 4: Frontend Embedding (Days 13-14)

#### 1. Web Handler Implementation
Following autobrr's pattern:

```go
// internal/web/handler.go
package web

import (
    "embed"
    "io/fs"
    "net/http"
    "github.com/go-chi/chi/v5"
)

//go:embed all:dist
var embedFS embed.FS

type Handler struct {
    fs      fs.FS
    baseURL string
    version string
}

func NewHandler(version, baseURL string) (*Handler, error) {
    distFS, err := fs.Sub(embedFS, "dist")
    if err != nil {
        return nil, err
    }

    return &Handler{
        fs:      distFS,
        baseURL: baseURL,
        version: version,
    }, nil
}

func (h *Handler) RegisterRoutes(r *chi.Mux) {
    // Serve static assets
    fileServer := http.FileServer(http.FS(h.fs))
    r.Handle("/assets/*", fileServer)
    
    // SPA catch-all route
    r.Get("/*", h.serveSPA)
}

func (h *Handler) serveSPA(w http.ResponseWriter, r *http.Request) {
    // Always serve index.html for SPA routes
    file, err := h.fs.Open("index.html")
    if err != nil {
        http.Error(w, "Not found", http.StatusNotFound)
        return
    }
    defer file.Close()
    
    stat, _ := file.Stat()
    http.ServeContent(w, r, "index.html", stat.ModTime(), file)
}
```

#### 2. Build Process
```makefile
# Makefile
.PHONY: build frontend backend

build: frontend backend

frontend:
	cd web && pnpm install && pnpm build
	cp -r web/dist internal/web/

backend:
	go build -ldflags "-X main.Version=$(VERSION)" -o qui ./cmd/qui

dev-backend:
	air -c .air.toml

dev-frontend:
	cd web && pnpm dev

clean:
	rm -rf web/dist internal/web/dist qui
```

### Phase 5: Performance Optimization for 10k+ Torrents (Days 15-16)

#### 1. Backend Optimizations

##### Core Strategy: SyncMainData Implementation
The key to handling 10k+ torrents efficiently is leveraging qBittorrent's `/api/v2/sync/maindata` endpoint through the go-qbittorrent library. This provides incremental updates instead of fetching full torrent lists.

##### Performance-Critical Features
- **Connection Pooling**: Reuse qBittorrent client connections with health checks
- **Request Batching**: Combine multiple torrent operations into single API calls
- **Intelligent Caching**: 
  - Use Ristretto for high-performance caching
  - Cache categories, tags, and instance metadata
  - TTL-based cache invalidation
- **Goroutine Pool**: Use ants/v2 for controlled concurrency
- **Database Optimizations**:
  ```sql
  -- Additional performance indexes
  CREATE INDEX idx_torrents_state ON torrents_cache(instance_id, state);
  CREATE INDEX idx_torrents_category ON torrents_cache(instance_id, category);
  CREATE INDEX idx_torrents_added ON torrents_cache(instance_id, added_on DESC);
  ```

##### Caching Strategy
```go
// High-performance cache configuration
cache, _ := ristretto.NewCache(&ristretto.Config{
    NumCounters: 1e7,     // 10 million
    MaxCost:     1 << 30, // 1GB
    BufferItems: 64,
})

// Cache keys with TTL (optimized for real-time updates)
// - Instance metadata: 5 minutes
// - Categories/tags: 1 minute  
// - Torrent list pages: 2 seconds (reduced for responsiveness)
// - Individual torrent details: 2 seconds (reduced for responsiveness)
// - Torrent count: 2 seconds (reduced for responsiveness)
```

##### Critical: Cache Invalidation for Real-time Updates
**Problem**: qBittorrent needs time to process actions (pause/resume/delete) before its API reflects changes.

**Solution**: Coordinated cache invalidation between backend and frontend:

```go
// Backend: Immediate cache invalidation after actions
func (h *TorrentsHandler) BulkAction(w http.ResponseWriter, r *http.Request) {
    // Perform action
    if err := h.syncManager.BulkAction(r.Context(), instanceID, req.Hashes, req.Action); err != nil {
        // Handle error
        return
    }
    
    // Immediately clear cache - next request gets fresh data
    h.syncManager.InvalidateCache(instanceID)
    
    // Return success immediately (good UX)
    RespondJSON(w, http.StatusOK, map[string]string{
        "message": "Bulk action completed successfully",
    })
}

// Cache invalidation clears entire cache (Ristretto limitation)
func (sm *SyncManager) InvalidateCache(instanceID int) {
    log.Debug().Int("instanceID", instanceID).Msg("Invalidating cache for instance")
    sm.cache.Clear() // Simple but effective approach
}
```

```tsx
// Frontend: Delayed invalidation to allow qBittorrent processing
const mutation = useMutation({
    mutationFn: (data) => api.bulkAction(instanceId, data),
    onSuccess: () => {
        // Wait for qBittorrent to process the change
        setTimeout(() => {
            queryClient.invalidateQueries({ 
                queryKey: ['torrents-list', instanceId],
                exact: false // Match all related queries
            })
        }, 1000) // 1 second for actions, 500ms for adding torrents
    },
})
```

**Key Timings**:
- Backend cache TTL: 2 seconds (reduced from 10-30 seconds)
- Frontend invalidation delay: 1000ms for actions, 500ms for adding torrents
- React Query stale time: 5 seconds (reduced from 30 seconds)

This ensures immediate UI feedback while guaranteeing data consistency.

#### 2. Frontend Optimizations

##### Server-Side Pagination Strategy
```tsx
// src/hooks/useTorrentsList.ts
export function useTorrentsList(instanceId: string, options = {}) {
  const [torrents, setTorrents] = useState<Torrent[]>([])
  const [currentPage, setCurrentPage] = useState(0)
  const pageSize = 500 // Load 500 torrents per page
  
  // Fetch page from server with filtering/sorting
  const { data, isLoading } = useQuery({
    queryKey: ['torrents-list', instanceId, currentPage, options.filters],
    queryFn: () => api.getTorrents(instanceId, { 
      page: currentPage, 
      limit: pageSize,
      sort: options.sort || 'addedOn',
      order: options.order || 'desc',
      filters: options.filters, // Server handles filtering
    }),
    staleTime: 2000, // Match backend cache TTL
    refetchInterval: 5000, // Poll for updates
  })
  
  // Append new pages to existing data
  useEffect(() => {
    if (data?.torrents) {
      if (currentPage === 0) {
        setTorrents(data.torrents)
      } else {
        setTorrents(prev => [...prev, ...data.torrents])
      }
    }
  }, [data, currentPage])
  
  const loadMore = () => {
    if (torrents.length < data?.total) {
      setCurrentPage(prev => prev + 1)
    }
  }
  
  return { 
    torrents, 
    totalCount: data?.total || 0,
    isLoading,
    loadMore,
    hasMore: torrents.length < (data?.total || 0)
  }
}
```

##### Virtual Scrolling for DOM Efficiency
```tsx
// Virtual scrolling handles DOM rendering efficiency
export function TorrentTable({ instanceId }: { instanceId: string }) {
  // Use server-side paginated data
  const { torrents, loadMore, hasMore } = useTorrentsList(instanceId, {
    pageSize: 500, // Load in chunks from server
  })
  
  // Virtual scrolling only renders visible rows in DOM
  const virtualizer = useVirtualizer({
    count: torrents.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 45,
    overscan: 10, // Render a few extra rows for smooth scrolling
  })
  
  // Load more from server when approaching end
  useEffect(() => {
    const lastItem = virtualizer.getVirtualItems().at(-1)
    if (lastItem && lastItem.index >= torrents.length - 50 && hasMore) {
      loadMore() // Fetch next page from server
    }
  }, [virtualizer.getVirtualItems(), torrents.length, hasMore, loadMore])
}
```

### Phase 6: Testing and Documentation (Days 17-18)

#### 1. Testing Strategy
- **Backend**: Table-driven tests for handlers
- **Frontend**: Component tests with React Testing Library
- **E2E**: Playwright tests for critical flows
- **Load Testing**: Simulate 10k+ torrents

#### 2. Documentation
- API documentation with OpenAPI/Swagger
- User guide for installation and configuration
- Developer documentation for contributing

## Technical Considerations

### go-qbittorrent Library Capabilities
The go-qbittorrent library provides excellent support for handling large-scale deployments:
- **SyncMainData Support**: Full implementation of `/api/v2/sync/maindata` endpoint for efficient incremental updates
- **Pagination**: Built-in support for `limit` and `offset` parameters in `GetTorrentsCtx`
- **Filtering**: Comprehensive filtering options (state, category, tag, hashes)
- **Response ID Tracking**: Proper RID management for incremental sync

**Limitations**:
- WebSocket/SSE support for real-time updates (mitigated by efficient polling with SyncMainData)
- Some bulk operations require custom implementation
- Advanced filtering may need client-side processing

### Performance Considerations for 10k+ Torrents

#### API Strategy
1. **Hybrid Data Loading**:
   - Initial load: Paginated `/api/v2/torrents/info` (100-200 torrents)
   - Real-time updates: `/api/v2/sync/maindata` for incremental changes
   - Never fetch all 10k+ torrents at once

2. **Request Optimization**:
   - Debounce search/filter operations (300ms)
   - Cancel in-flight requests when parameters change
   - Use AbortController for proper cleanup

3. **Memory Management**:
   - Server-side pagination prevents loading all torrents into memory
   - Backend handles filtering/sorting to reduce data transfer
   - Virtual scrolling (TanStack Virtual) ensures only visible rows are rendered in DOM

#### Monitoring & Metrics
- Track API response times
- Monitor memory usage
- Log slow queries (>100ms)
- Alert on sync failures

### React 19 Key Features
- **New JSX Transform**: Required for React 19 (automatic runtime)
- **Optimistic Updates**: useOptimistic hook for immediate UI feedback
- **use() API**: Suspend on promises and read resources in render
- **Refs as Props**: No more forwardRef needed
- **Breaking Changes**: Removed legacy APIs (propTypes, defaultProps, string refs)

### TanStack Form Best Practices
- **Type Safety**: Full TypeScript support with deep inference for complex forms
- **Headless UI**: Complete control over rendering, integrates perfectly with shadcn/ui
- **Validation**: Synchronous, asynchronous, and schema-based validation support
- **Performance**: Granular reactivity ensures only changed fields re-render
- **Complex Forms**: Ideal for settings, multi-step forms, and dynamic field arrays

### TanStack Table v8 Best Practices
- **Server-Side Operations**: For 10k+ rows, use manualPagination, manualSorting, manualFiltering
- **Hybrid Approach**: Server-side data processing + client-side virtualization
- **State Management**: Control pagination, sorting, and filtering state externally
- **Performance**: TanStack Virtual integration for rendering only visible rows
- **Data Fetching**: Coordinate with TanStack Query for optimal caching
- **New v8.14.0 Feature**: `_features` option for custom feature integration
- **API Updates**: Use `flexRender()` instead of deprecated `cell.render()`
- **Column Definitions**: Use `accessorKey` (not `accessor`), `size` (not `width`)
- **Row Models**: Import tree-shakable row models explicitly (order doesn't matter)

### Tailwind CSS v4 Changes
- CSS-first configuration approach using @theme and @config
- Built-in Vite plugin (@tailwindcss/vite)
- Automatic content detection (no manual content config)
- Native CSS variables for all design tokens
- 5x faster builds with Rust-based Oxide engine

### shadcn/ui Integration
- Install components on-demand
- Maintain default styling (no custom modifications)
- Essential components for the app:
  - Form components: input, label, button, select, checkbox, switch
  - Layout: card, separator, sidebar, sheet
  - Feedback: toast (sonner), alert, progress
  - Data display: table, badge
- Components requiring Radix UI primitives:
  - dropdown-menu (@radix-ui/react-dropdown-menu)
  - context-menu (@radix-ui/react-context-menu)
  - dialog (@radix-ui/react-dialog)
  - select (@radix-ui/react-select)
  - switch (@radix-ui/react-switch)
  - checkbox (@radix-ui/react-checkbox)

### Single Binary Distribution
- Use Go's embed directive for frontend assets
- Embed after Vite build process
- Serve embedded files through Chi router
- No external dependencies required

## Security Considerations

1. **Authentication**
   - Argon2id for password hashing
   - Secure session cookies (HttpOnly, Secure, SameSite)
   - Session secret configuration via env/config
   - API keys for programmatic access
   - Session configuration: `qui_SESSION_SECRET` or `sessionSecret` in config.toml

2. **Data Protection**
   - Encrypt qBittorrent credentials in database
   - Use HTTPS in production
   - Sanitize all user inputs

3. **API Security**
   - Rate limiting on authentication endpoints
   - CORS configuration for API access
   - Input validation on all endpoints

## Configuration

### Configuration Management (Viper-based)
Following autobrr's pattern, qui uses Viper for flexible configuration management:

1. **Configuration Priority** (highest to lowest):
   - Environment variables (prefixed with `qui__`)
   - Configuration file (`config.toml`)
   - Default values in code

2. **Container Detection**: Automatically detects container environments (Docker, LXC) and adjusts defaults:
   - Host defaults to `0.0.0.0` in containers, `127.0.0.1` otherwise
   - Checks for `/.dockerenv`, `/dev/.lxc-boot-id`, or PID 1

3. **First Run**: Creates `config.toml` with secure defaults if not present

### Configuration Implementation
```go
// internal/domain/config.go
type Config struct {
    Host          string `toml:"host"`
    Port          int    `toml:"port"`
    BaseURL       string `toml:"baseUrl"`
    SessionSecret string `toml:"sessionSecret"`
    LogLevel      string `toml:"logLevel"`
    LogPath       string `toml:"logPath"`
    DatabasePath  string `toml:"databasePath"`
}

// internal/config/config.go
func (c *AppConfig) defaults() {
    c.Config = &domain.Config{
        Host:         "localhost",
        Port:         8080,
        LogLevel:     "INFO",
        DatabasePath: "./data/qui.db",
        SessionSecret: api.GenerateSecureToken(16),
    }
}

// Container detection for host default
func detectContainer() bool {
    // Check Docker
    if _, err := os.Stat("/.dockerenv"); err == nil {
        return true
    }
    // Check LXC
    if _, err := os.Stat("/dev/.lxc-boot-id"); err == nil {
        return true
    }
    // Check if running as init
    if os.Getpid() == 1 {
        return true
    }
    return false
}
```

### Environment Variables
```bash
# All environment variables use qui__ prefix (double underscore)
# Server configuration
qui__HOST=0.0.0.0
qui__PORT=8080
qui__BASE_URL=/

# Session secret (auto-generated if not set)
qui__SESSION_SECRET=your-secret-key-here

# Database
qui__DATABASE_PATH=/path/to/qui.db

# Logging
qui__LOG_LEVEL=INFO  # Options: ERROR, DEBUG, INFO, WARN, TRACE
qui__LOG_PATH=/path/to/qui.log  # If not set, logs to stdout
```

### Config File Template (config.toml)
```toml
# config.toml - Auto-generated on first run

# Hostname / IP
# Default: "localhost" (or "0.0.0.0" in containers)
host = "{{ .host }}"

# Port
# Default: 8080
port = 8080

# Base URL
# Set custom baseUrl eg /qui/ to serve in subdirectory.
# Not needed for subdomain, or by accessing with :port directly.
# Optional
#baseUrl = "/qui/"

# Session secret
# Auto-generated if not provided
sessionSecret = "{{ .sessionSecret }}"

# Database path
# Default: "./data/qui.db"
databasePath = "./data/qui.db"

# Log file path
# If not defined, logs to stdout
# Optional
#logPath = "log/qui.log"

# Log level
# Default: "INFO"
# Options: "ERROR", "DEBUG", "INFO", "WARN", "TRACE"
logLevel = "INFO"
```

### Dynamic Configuration Reload
- Supports live reloading of certain settings without restart:
  - Log level changes take effect immediately
  - Log path changes redirect output
- Uses fsnotify to watch `config.toml` for changes

## Deployment Strategy

1. **Docker Support**
   ```dockerfile
   FROM node:20-alpine AS frontend-builder
   WORKDIR /app
   COPY web/package.json web/pnpm-lock.yaml ./
   RUN npm install -g pnpm && pnpm install
   COPY web/ ./
   RUN pnpm build

   FROM golang:1.24-alpine AS backend-builder
   WORKDIR /app
   COPY go.mod go.sum ./
   RUN go mod download
   COPY . .
   COPY --from=frontend-builder /app/dist ./internal/web/dist
   RUN go build -o qui ./cmd/qui

   FROM alpine:latest
   RUN apk --no-cache add ca-certificates
   COPY --from=backend-builder /app/qui /qui
   EXPOSE 8080
   CMD ["/qui"]
   ```

2. **Cross-platform Builds**
   - Use goreleaser for automated releases
   - Support linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64

## Success Metrics

1. **Deployment Simplicity** (Self-Hosted Focus)
   - Single binary deployment with no external dependencies
   - Zero-configuration startup with sensible defaults
   - Works out-of-the-box on common homelab setups
   - Simple backup/restore (just copy the binary and database)

2. **Performance**
   - Handle 10k+ torrents with sub-100ms response times using SyncMainData
   - Initial page load < 1 second (first 100 torrents)
   - Incremental updates every 2 seconds with minimal bandwidth
   - Memory usage < 500MB even with 10k+ torrents
   - Smooth 60fps scrolling with virtual rendering

3. **Usability**
   - Intuitive UI matching qBittorrent's layout
   - Responsive design for various screen sizes
   - Keyboard shortcuts for power users
   - No complex user management - just login and use

4. **Reliability**
   - Automatic reconnection to instances
   - Graceful error handling
   - Data consistency across multiple instances
   - Minimal maintenance required

## Timeline Summary

- **Week 1**: Project setup and core backend
- **Week 2**: Frontend implementation and integration
- **Week 3**: Performance optimization and testing
- **Total**: 18 days for MVP

## Next Steps

1. Create GO_QBITTORRENT_ENHANCEMENTS.md documenting missing features
2. Set up CI/CD pipeline with GitHub Actions
3. Implement monitoring and logging
4. Plan post-MVP features (RSS, statistics, mobile UI)