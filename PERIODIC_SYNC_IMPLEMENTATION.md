# Periodic Cache Refresh Implementation

## Overview
This document describes the implementation of automatic periodic cache refresh for qBittorrent instances in qui. The feature allows instances to automatically sync their cache at configurable intervals (measured in minutes, with a 1-minute minimum). The periodic sync timer **resets after every sync operation**, ensuring the interval represents "time since last sync" rather than a fixed schedule.

## Key Behavior

**Timer Reset on ANY Sync**: The periodic sync timer resets whenever a sync occurs from any source:
- ✅ Periodic sync triggers → Timer resets
- ✅ User manually refreshes from frontend → Timer resets  
- ✅ Proxy performs a sync → Timer resets
- ✅ Any operation that triggers sync manager → Timer resets

This prevents unnecessary syncs and ensures the configured interval represents the **minimum time between syncs**, not a rigid schedule that ignores other sync activity.

## Architecture

### Database Layer
- **Migration**: `010_add_sync_interval.sql`
  - Adds `sync_interval` INTEGER column to `instances` table
  - Default value: 0 (disabled)
  - Values: 0 = disabled, 1+ = minutes between syncs
  - Minimum enforced: 1 minute

### Model Layer (`internal/models/instance.go`)
- Added `SyncInterval int` field to `Instance` struct
- Updated JSON marshaling/unmarshaling to include sync_interval
- Updated all CRUD operations:
  - `Create`: Accepts syncInterval parameter
  - `Get`: Selects sync_interval from database
  - `List`: Selects sync_interval from database
  - `Update`: Accepts and updates sync_interval

### API Layer (`internal/api/handlers/instances.go`)
- Updated request/response structs:
  - `CreateInstanceRequest`: Added `SyncInterval int` field
  - `UpdateInstanceRequest`: Added `SyncInterval *int` field (optional)
  - `InstanceResponse`: Added `SyncInterval int` field
- Added validation:
  - Both Create and Update handlers validate sync_interval
  - Must be 0 (disabled) or >= 1 minute
- Updated handlers:
  - `CreateInstance`: Passes syncInterval to Create method
  - `UpdateInstance`: Passes syncInterval to Update method and calls UpdateClientSyncInterval

### Client Layer (`internal/qbittorrent/client.go`)
- Added periodic sync fields to `Client` struct:
  - `syncInterval int`: Minutes between automatic syncs (0 = disabled)
  - `syncTimer *time.Timer`: Timer for next periodic sync
  - `lastSyncTime time.Time`: Time of last sync (from any source)
  - `syncCtx context.Context`: Dedicated context for sync manager lifecycle
  - `syncCancel context.CancelFunc`: Function to stop sync manager
  - `syncMu sync.RWMutex`: Protects sync timer and timing fields

- **Key Methods**:
  - `StartSyncManager(ctx, syncInterval)`: Creates and starts sync manager with custom periodic sync
    - Creates dedicated context using `context.Background()` (not request context)
    - Configures `SyncOptions` with `DynamicSync=true` and `AutoSync=false`
    - Sets up OnUpdate callback to call `recordSyncTime()` after every sync
    - Starts custom periodic sync timer if interval >= 1
  - `startPeriodicSync()`: Starts the periodic sync timer
  - `performPeriodicSync()`: Executes a periodic sync
  - `recordSyncTime()`: Records sync time and **resets the timer** (called after ANY sync)
  - `UpdateSyncInterval(syncInterval)`: Updates interval by stopping timer and recreating sync manager
  - `StartSyncManagerLegacy(ctx)`: Backward-compatible wrapper (0 interval)

- **Timer Reset Logic**:
  - `recordSyncTime()` is called in the `OnUpdate` callback
  - This means the timer resets on **every successful sync**, regardless of source:
    - Periodic sync via our timer
    - Manual sync from frontend (torrent table)
    - Sync triggered by proxy operations
    - Any other sync operation
  - Timer is stopped and restarted with full interval duration

### Pool Layer (`internal/qbittorrent/pool.go`)
- Updated `createClientWithTimeout`:
  - Passes instance.SyncInterval to StartSyncManager
- Added `UpdateClientSyncInterval(instanceID, syncInterval)`:
  - Updates sync interval for existing clients in pool
  - Called when instance settings are updated

## Behavior

### Sync Interval Rules
1. **0**: Periodic sync disabled (default)
2. **1+ minutes**: Valid, enables periodic sync with timer reset

### Timer Reset Behavior
The periodic sync timer is **reset on every sync operation**, ensuring the countdown always starts from the last successful sync:

1. **Periodic sync triggers** → Sync executes → OnUpdate called → recordSyncTime() → Timer resets
2. **User manually refreshes via frontend** → Sync executes → OnUpdate called → recordSyncTime() → Timer resets
3. **Proxy performs sync** → Sync executes → OnUpdate called → recordSyncTime() → Timer resets
4. **Any operation that triggers sync** → OnUpdate called → recordSyncTime() → Timer resets

This prevents redundant syncs and ensures the interval represents **"time since last sync from any source"** rather than a fixed schedule.

### Example Timeline
If sync_interval = 10 minutes:
```
T+0:  Client created → Initial sync → Timer starts (next sync at T+10)
T+5:  User manually refreshes → Sync occurs → Timer resets (next sync at T+15)
T+7:  Proxy performs sync → Sync occurs → Timer resets (next sync at T+17)
T+17: No activity → Periodic sync triggers → Timer resets (next sync at T+27)
T+27: Periodic sync triggers again
```

Note: Manual syncs don't interrupt the timer countdown - they reset it to a fresh interval.

### Client Lifecycle
1. **Client Creation**: 
   - syncInterval loaded from database
   - Passed to StartSyncManager
   - SyncManager created with DynamicSync enabled, AutoSync disabled
   - Dedicated context (`syncCtx`) created from `context.Background()`
   - If syncInterval >= 1, custom periodic sync timer started

2. **Instance Update**:
   - API handler updates database
   - Calls UpdateClientSyncInterval on pool
   - Existing timer stopped
   - Existing context canceled (stops sync manager)
   - New SyncManager created with updated settings
   - New timer started with new interval
   - Client removed from pool (forces reconnect with new settings)

3. **Sync Operations**:
   - **Periodic Sync**: Happens when timer fires (custom timer, not library AutoSync)
   - **Manual Sync**: User-initiated from frontend (torrent table refresh button)
   - **Proxy Sync**: Triggered by proxy operations
   - **All syncs** trigger OnUpdate callback → recordSyncTime() → Timer resets

## Frontend Integration

### Instance Settings Modal
The frontend should add:
```typescript
// In the instance form
{
  label: "Sync Interval (minutes)",
  type: "number",
  min: 0,
  step: 1,
  placeholder: "0 (disabled) or 1+ minutes",
  helperText: "Automatically refresh cache every N minutes. Timer resets after any sync. Minimum 1 minute, 0 to disable."
}
```

### API Request/Response
```json
// POST /api/instances
{
  "name": "Instance Name",
  "host": "http://localhost:8080",
  "username": "admin",
  "password": "password",
  "syncInterval": 10  // Minutes
}

// Response
{
  "id": 1,
  "name": "Instance Name",
  "syncInterval": 10,
  ...
}
```

## Implementation Files Changed

### Database
- `internal/database/migrations/010_add_sync_interval.sql` (new)

### Models
- `internal/models/instance.go`
  - Instance struct
  - MarshalJSON/UnmarshalJSON
  - Create/Get/List/Update methods

### API
- `internal/api/handlers/instances.go`
  - CreateInstanceRequest
  - UpdateInstanceRequest  
  - InstanceResponse
  - CreateInstance handler (validation)
  - UpdateInstance handler (validation + update)
  - buildInstanceResponse
  - buildQuickInstanceResponse

### qBittorrent Client
- `internal/qbittorrent/client.go`
  - Client struct (added syncInterval, syncTimer, lastSyncTime, syncCtx, syncCancel fields)
  - StartSyncManager (creates SyncManager with DynamicSync, starts custom periodic sync timer)
  - startPeriodicSync (starts the timer for periodic syncs)
  - performPeriodicSync (executes periodic sync when timer fires)
  - recordSyncTime (records sync time and resets timer - called after ANY sync)
  - UpdateSyncInterval (stops timer, recreates SyncManager with new interval)
  - Custom timer-based implementation that resets after any sync
  - Does NOT use go-qbittorrent's AutoSync (would run on fixed schedule)

### Client Pool
- `internal/qbittorrent/pool.go`
  - createClientWithTimeout (pass syncInterval)
  - UpdateClientSyncInterval (new)

### Tests
- `internal/models/instance_test.go`
  - Updated Create/Update test calls to include syncInterval

## Testing

### Manual Testing Steps
1. **Create instance with sync interval**:
   - POST to /api/instances with syncInterval: 1 or higher
   - Verify database has sync_interval = configured value
   - Check logs for "Started periodic background syncing" message

2. **Verify periodic sync**:
   - Wait for the configured interval
   - Check logs for "Performing periodic cache refresh"
   - Verify next sync is scheduled

3. **Verify timer reset on manual sync**:
   - Set sync interval to 10 minutes
   - Wait 3 minutes
   - Manually refresh from frontend
   - Check logs for "Periodic sync timer reset after sync"
   - Wait - next periodic sync should occur 10 minutes after manual sync, not 7

4. **Update sync interval**:
   - PATCH instance with syncInterval: different value
   - Verify logs show "Updating periodic sync interval"
   - Old timer should stop, new one should start with new duration

5. **Disable sync**:
   - PATCH instance with syncInterval: 0
   - Verify timer is stopped, no more periodic syncs

6. **Validation**:
   - Try creating with syncInterval: 0.5
   - Should get 400 error: "must be 0 or at least 1 minute"

7. **Background Operation**:
   - Start instance with sync interval
   - Close all frontend connections
   - Verify syncing continues in background (check logs)
   - Uses context.Background() so not tied to request lifecycle

### Integration Testing
```bash
# Build and run
go build -v ./...
./qui

# Test API
curl -X POST http://localhost:7476/api/instances \
  -H "Content-Type: application/json" \
  -d '{"name":"Test","host":"http://localhost:8080","username":"admin","password":"admin","syncInterval":5}'

# Verify logs
# Should see: "Started periodic background syncing" with intervalMinutes configured
# Should see: "Sync manager started successfully" with periodicSyncEnabled=true
```

## Architecture Notes

### Why Custom Timer Instead of go-qbittorrent's AutoSync?

**go-qbittorrent's AutoSync:**
- Runs on a **fixed schedule** with `time.After(interval)`
- Syncs occur at regular intervals regardless of other sync activity
- Example: If interval is 10 minutes, syncs happen at T+10, T+20, T+30...
- Manual syncs or proxy syncs don't affect the schedule

**Our Custom Timer Implementation:**
- Timer **resets after every sync** from any source
- Ensures configured interval is **minimum time between syncs**
- Prevents redundant syncs when other operations already triggered sync
- Example: If interval is 10 minutes and user syncs manually at T+5, next periodic sync is at T+15 (not T+10)

### Timer Reset Mechanism

The `recordSyncTime()` method is called in the `OnUpdate` callback, which fires after **every successful sync**:

```go
syncOpts.OnUpdate = func(data *qbt.MainData) {
    c.updateHealthStatus(true)
    c.updateServerState(data)
    c.rebuildHashIndex(data.Torrents)
    c.recordSyncTime() // ← Resets timer after ANY sync
    log.Debug()...
}
```

This means:
- ✅ Periodic sync triggers → OnUpdate → Timer resets
- ✅ User clicks refresh → Sync → OnUpdate → Timer resets
- ✅ Proxy performs sync → OnUpdate → Timer resets
- ✅ Any sync operation → OnUpdate → Timer resets

### Context Management

**Critical Design Decision:**
The sync manager context is based on `context.Background()` rather than HTTP request contexts:

```go
// CORRECT: Uses Background() for long-lived operation
c.syncCtx, c.syncCancel = context.WithCancel(context.Background())

// WRONG: Would stop when HTTP request ends
syncManager.Start(ctx) // where ctx is from HTTP handler
```

This ensures the sync manager and periodic syncs continue running even when:
- No users are connected to the frontend
- HTTP requests complete
- WebSocket connections close
- Dynamic syncs finish

### Timer vs Ticker

We use `time.Timer` (via `time.AfterFunc`) instead of `time.Ticker`:

**Timer** (what we use):
- Single-shot, fires once
- Can be stopped and reset with new duration
- Perfect for "reset after sync" behavior
- `recordSyncTime()` stops old timer and creates new one

**Ticker** (what we don't use):
- Fires repeatedly at fixed intervals
- Can't easily adjust duration without recreating
- Would require manual reset logic on every sync
- Not ideal for our use case

## Future Enhancements

1. **Per-Instance Status**: Show time until next sync in UI
2. **Sync History**: Track sync success/failure rate
3. **Adaptive Intervals**: Adjust interval based on activity
4. **Sync on Demand**: Manual trigger button in UI (already exists via DynamicSync)
5. **Health-Based Sync**: Skip sync if instance is unhealthy

## Notes

- Minimum 1 minute chosen as reasonable lower bound for periodic syncing
- Timer resets on every sync to prevent redundant syncs after manual/proxy operations
- SyncManager recreation on interval change ensures clean state
- Backward compatible: existing instances default to 0 (disabled)
- Goroutine-safe: all timer operations protected by syncMu
- Custom timer implementation instead of go-qbittorrent's AutoSync
- True background operation via context.Background() based context
- Timer ensures configured interval is **minimum time between syncs**
