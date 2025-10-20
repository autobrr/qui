# Periodic Cache Refresh Implementation

## Overview
This document describes the implementation of automatic periodic cache refresh for qBittorrent instances in qui. The feature allows instances to automatically sync their cache at configurable intervals (measured in minutes, with a 5-minute minimum).

## Architecture

### Database Layer
- **Migration**: `010_add_sync_interval.sql`
  - Adds `sync_interval` INTEGER column to `instances` table
  - Default value: 0 (disabled)
  - Values: 0 = disabled, 5+ = minutes between syncs
  - Minimum enforced: 5 minutes

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
  - Must be 0 (disabled) or >= 5 minutes
- Updated handlers:
  - `CreateInstance`: Passes syncInterval to Create method
  - `UpdateInstance`: Passes syncInterval to Update method and calls UpdateClientSyncInterval

### Client Layer (`internal/qbittorrent/client.go`)
- Added periodic sync fields to `Client` struct:
  - `syncInterval int`: Minutes between automatic syncs
  - `syncTicker *time.Ticker`: Ticker for periodic syncs
  - `stopSync chan struct{}`: Channel to stop sync ticker
  - `lastSyncTime time.Time`: Time of last sync
  - `syncMu sync.RWMutex`: Protects sync ticker fields

- **Key Methods**:
  - `StartSyncManager(ctx, syncInterval)`: Starts sync manager with periodic sync
  - `startPeriodicSync()`: Starts the ticker for periodic syncs
  - `performPeriodicSync()`: Executes a sync operation
  - `recordSyncTime()`: Records sync time and resets ticker
  - `UpdateSyncInterval(syncInterval)`: Updates interval and restarts ticker
  - `StartSyncManagerLegacy(ctx)`: Backward-compatible wrapper (0 interval)

- **Ticker Reset Logic**:
  - Ticker is reset on **every** sync, not just periodic ones
  - Added `recordSyncTime()` call in `OnUpdate` callback
  - This ensures the interval countdown restarts whenever:
    - Manual sync occurs via frontend
    - Periodic sync triggers
    - Any operation that forces a sync (add, delete, etc.)

### Pool Layer (`internal/qbittorrent/pool.go`)
- Updated `createClientWithTimeout`:
  - Passes instance.SyncInterval to StartSyncManager
- Added `UpdateClientSyncInterval(instanceID, syncInterval)`:
  - Updates sync interval for existing clients in pool
  - Called when instance settings are updated

## Behavior

### Sync Interval Rules
1. **0**: Periodic sync disabled (default)
2. **1-4 minutes**: Invalid, rejected by API validation
3. **5+ minutes**: Valid, enables periodic sync

### Ticker Reset Behavior
The periodic sync ticker is reset on **every sync operation**, ensuring the countdown always starts from the last successful sync:

1. **Periodic sync triggers** → Timer resets
2. **User manually refreshes** → Timer resets
3. **Torrent added/deleted** → Forces sync → Timer resets
4. **Any operation calling sync manager** → Timer resets

This prevents unnecessary syncs and ensures the interval represents "time since last sync" rather than a fixed schedule.

### Example Timeline
If sync_interval = 10 minutes:
```
T+0:  User opens dashboard → Sync occurs → Timer starts
T+5:  User adds torrent → Sync occurs → Timer resets to 0
T+10: No activity → Periodic sync triggers → Timer resets
T+20: Periodic sync triggers again
```

### Client Lifecycle
1. **Client Creation**: 
   - syncInterval loaded from database
   - Passed to StartSyncManager
   - Ticker started if syncInterval >= 5

2. **Instance Update**:
   - API handler updates database
   - Calls UpdateClientSyncInterval on pool
   - Existing client updates its ticker
   - Client removed from pool (forces reconnect with new settings)

3. **Sync Operations**:
   - Every successful sync calls recordSyncTime()
   - Ticker is reset to full interval
   - Countdown starts from current time

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
  placeholder: "0 (disabled) or 5+ minutes",
  helperText: "Automatically refresh cache every N minutes. Minimum 5 minutes, 0 to disable."
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
  - Client struct (new fields)
  - NewClientWithTimeout (initialize stopSync channel)
  - StartSyncManager (accept syncInterval, start ticker)
  - startPeriodicSync (new)
  - performPeriodicSync (new)
  - recordSyncTime (new, called on every sync)
  - UpdateSyncInterval (new)
  - OnUpdate callback (added recordSyncTime call)

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
   - POST to /api/instances with syncInterval: 5
   - Verify database has sync_interval = 5
   - Check logs for "Started periodic cache refresh" message

2. **Verify periodic sync**:
   - Wait 5 minutes
   - Check logs for "Performing periodic cache refresh"
   - Verify ticker reset after manual sync

3. **Update sync interval**:
   - PATCH instance with syncInterval: 10
   - Verify logs show "Updating periodic sync interval"
   - Old ticker should stop, new one should start

4. **Disable sync**:
   - PATCH instance with syncInterval: 0
   - Verify logs show "Stopped periodic cache refresh"

5. **Validation**:
   - Try creating with syncInterval: 3
   - Should get 400 error: "must be 0 or at least 5 minutes"

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
# Should see: "Started periodic cache refresh" with intervalMinutes=5
```

## Future Enhancements

1. **Per-Instance Status**: Show time until next sync in UI
2. **Sync History**: Track sync success/failure rate
3. **Adaptive Intervals**: Adjust interval based on activity
4. **Sync on Demand**: Manual trigger button in UI
5. **Health-Based Sync**: Skip sync if instance is unhealthy

## Notes

- Minimum 5 minutes chosen to prevent excessive API calls to qBittorrent
- Ticker resets on every sync to prevent redundant syncs after manual refresh
- Client recreation on update ensures all settings are fresh
- Backward compatible: existing instances default to 0 (disabled)
- Goroutine-safe: all ticker operations protected by syncMu
