# qBittorrent App Preferences Implementation Plan

## Overview
Implement support for fetching and managing qBittorrent App Preferences, making them available throughout the application via cached state management.

## Backend Implementation

### 1. SyncManager Methods (`internal/qbittorrent/sync_manager.go`)

Add two new methods following the existing caching patterns:

```go
// GetAppPreferences fetches and caches app preferences for an instance
func (sm *SyncManager) GetAppPreferences(ctx context.Context, instanceID int) (qbt.AppPreferences, error) {
    // Check cache with 60-second TTL (same as categories/tags)
    cacheKey := fmt.Sprintf("app_preferences:%d", instanceID)
    if cached, found := sm.cache.Get(cacheKey); found {
        if prefs, ok := cached.(qbt.AppPreferences); ok {
            return prefs, nil
        }
    }
    
    // Get client and fetch preferences
    client, err := sm.clientPool.GetClient(ctx, instanceID)
    if err != nil {
        return qbt.AppPreferences{}, fmt.Errorf("failed to get client: %w", err)
    }
    
    prefs, err := client.GetAppPreferencesCtx(ctx)
    if err != nil {
        return qbt.AppPreferences{}, fmt.Errorf("failed to get app preferences: %w", err)
    }
    
    // Cache for 60 seconds
    sm.cache.SetWithTTL(cacheKey, prefs, 1, 60*time.Second)
    
    return prefs, nil
}

// SetAppPreferences updates app preferences and invalidates cache
func (sm *SyncManager) SetAppPreferences(ctx context.Context, instanceID int, prefs map[string]interface{}) error {
    client, err := sm.clientPool.GetClient(ctx, instanceID)
    if err != nil {
        return fmt.Errorf("failed to get client: %w", err)
    }
    
    if err := client.SetPreferencesCtx(ctx, prefs); err != nil {
        return fmt.Errorf("failed to set preferences: %w", err)
    }
    
    // Invalidate cache
    cacheKey := fmt.Sprintf("app_preferences:%d", instanceID)
    sm.cache.Del(cacheKey)
    
    return nil
}
```

### 2. Preferences Handler (`internal/api/handlers/preferences.go`)

Create a new handler for preferences endpoints:

```go
package handlers

type PreferencesHandler struct {
    syncManager *qbittorrent.SyncManager
}

func NewPreferencesHandler(syncManager *qbittorrent.SyncManager) *PreferencesHandler {
    return &PreferencesHandler{
        syncManager: syncManager,
    }
}

// GetPreferences returns app preferences for an instance
func (h *PreferencesHandler) GetPreferences(w http.ResponseWriter, r *http.Request) {
    instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
    if err != nil {
        http.Error(w, "Invalid instance ID", http.StatusBadRequest)
        return
    }
    
    prefs, err := h.syncManager.GetAppPreferences(r.Context(), instanceID)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(prefs)
}

// UpdatePreferences updates specific preference fields
func (h *PreferencesHandler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
    instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
    if err != nil {
        http.Error(w, "Invalid instance ID", http.StatusBadRequest)
        return
    }
    
    var prefs map[string]interface{}
    if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
        http.Error(w, "Invalid request body", http.StatusBadRequest)
        return
    }
    
    if err := h.syncManager.SetAppPreferences(r.Context(), instanceID, prefs); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    // Return updated preferences
    updatedPrefs, err := h.syncManager.GetAppPreferences(r.Context(), instanceID)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(updatedPrefs)
}
```

### 3. Add Routes (`internal/api/router.go`)

Add preferences routes under the protected instance routes:

```go
// In NewRouter function, create handler:
preferencesHandler := handlers.NewPreferencesHandler(deps.SyncManager)

// Under r.Route("/instances/{instanceID}", ...):
r.Get("/preferences", preferencesHandler.GetPreferences)
r.Patch("/preferences", preferencesHandler.UpdatePreferences)
```

## Frontend Implementation

### 4. API Client Methods (`web/src/lib/api.ts`)

Add methods to fetch and update preferences:

```typescript
async getInstancePreferences(instanceId: number): Promise<AppPreferences> {
  return this.request<AppPreferences>(`/instances/${instanceId}/preferences`)
}

async updateInstancePreferences(
  instanceId: number, 
  preferences: Partial<AppPreferences>
): Promise<AppPreferences> {
  return this.request<AppPreferences>(`/instances/${instanceId}/preferences`, {
    method: "PATCH",
    body: JSON.stringify(preferences),
  })
}
```

### 5. TypeScript Types (`web/src/types/index.ts`)

Define the AppPreferences interface:

```typescript
export interface AppPreferences {
  // Download/Upload limits
  dl_limit: number
  up_limit: number
  alt_dl_limit: number
  alt_up_limit: number
  
  // Queue management
  queueing_enabled: boolean
  max_active_downloads: number
  max_active_torrents: number
  max_active_uploads: number
  
  // Network settings
  listen_port: number
  random_port: boolean
  upnp: boolean
  
  // Connection limits
  max_connec: number
  max_connec_per_torrent: number
  max_uploads: number
  max_uploads_per_torrent: number
  
  // Seeding limits
  max_ratio_enabled: boolean
  max_ratio: number
  max_seeding_time_enabled: boolean
  max_seeding_time: number
  
  // Paths
  save_path: string
  temp_path: string
  temp_path_enabled: boolean
  
  // Auto management
  auto_tmm_enabled: boolean
  
  // BitTorrent settings
  dht: boolean
  pex: boolean
  lsd: boolean
  encryption: number
  anonymous_mode: boolean
  
  // Scheduler
  scheduler_enabled: boolean
  schedule_from_hour: number
  schedule_from_min: number
  schedule_to_hour: number
  schedule_to_min: number
  scheduler_days: number
  
  // Web UI settings (read-only for reference)
  web_ui_port: number
  web_ui_username: string
  use_https: boolean
  
  // Add other fields as needed...
}
```

### 6. React Hook (`web/src/hooks/useInstancePreferences.ts`)

Create a hook for managing preferences:

```typescript
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { AppPreferences } from "@/types"

export function useInstancePreferences(instanceId: number | undefined) {
  const queryClient = useQueryClient()
  
  const { data: preferences, isLoading, error } = useQuery({
    queryKey: ["instance-preferences", instanceId],
    queryFn: () => instanceId ? api.getInstancePreferences(instanceId) : null,
    enabled: !!instanceId,
    staleTime: 5000, // 5 seconds
    refetchInterval: 60000, // Refetch every minute
  })
  
  const updateMutation = useMutation({
    mutationFn: (preferences: Partial<AppPreferences>) => {
      if (!instanceId) throw new Error("No instance ID")
      return api.updateInstancePreferences(instanceId, preferences)
    },
    onMutate: async (newPreferences) => {
      // Cancel outgoing refetches
      await queryClient.cancelQueries({ 
        queryKey: ["instance-preferences", instanceId] 
      })
      
      // Snapshot previous value
      const previousPreferences = queryClient.getQueryData<AppPreferences>(
        ["instance-preferences", instanceId]
      )
      
      // Optimistically update
      if (previousPreferences) {
        queryClient.setQueryData(
          ["instance-preferences", instanceId],
          { ...previousPreferences, ...newPreferences }
        )
      }
      
      return { previousPreferences }
    },
    onError: (err, newPreferences, context) => {
      // Rollback on error
      if (context?.previousPreferences) {
        queryClient.setQueryData(
          ["instance-preferences", instanceId],
          context.previousPreferences
        )
      }
    },
    onSuccess: () => {
      // Invalidate and refetch
      queryClient.invalidateQueries({ 
        queryKey: ["instance-preferences", instanceId] 
      })
    },
  })
  
  return {
    preferences,
    isLoading,
    error,
    updatePreferences: updateMutation.mutate,
    isUpdating: updateMutation.isPending,
  }
}
```

## Usage Examples

### Reading Preferences in Components

```typescript
function TorrentSettings({ instanceId }: { instanceId: number }) {
  const { preferences, isLoading } = useInstancePreferences(instanceId)
  
  if (isLoading) return <Spinner />
  
  return (
    <div>
      <p>Download limit: {preferences?.dl_limit || 0} KB/s</p>
      <p>Max active downloads: {preferences?.max_active_downloads}</p>
      <p>Queue enabled: {preferences?.queueing_enabled ? "Yes" : "No"}</p>
    </div>
  )
}
```

### Updating Preferences

```typescript
function SpeedLimitControl({ instanceId }: { instanceId: number }) {
  const { preferences, updatePreferences } = useInstancePreferences(instanceId)
  
  const handleLimitChange = (newLimit: number) => {
    updatePreferences({ dl_limit: newLimit })
  }
  
  return (
    <Slider
      value={preferences?.dl_limit || 0}
      onChange={handleLimitChange}
      max={100000}
    />
  )
}
```

### AddTorrentDialog Default Values Integration

```typescript
// In AddTorrentDialog.tsx - use preferences for smart defaults
function AddTorrentDialog({ instanceId }: { instanceId: number }) {
  const { preferences } = useInstancePreferences(instanceId)
  
  const form = useForm({
    defaultValues: {
      // Use instance preferences as defaults
      startPaused: preferences?.start_paused_enabled ?? false,
      savePath: preferences?.save_path || "",
      // Auto TMM enabled means use category paths
      useAutoTMM: preferences?.auto_tmm_enabled ?? true,
      category: "",
      tags: [],
    }
  })
  
  // Show/hide savePath input based on auto TMM setting
  const showSavePath = !preferences?.auto_tmm_enabled
  
  return (
    <form>
      {showSavePath && (
        <Input
          label="Save Path"
          value={form.state.values.savePath}
          placeholder={preferences?.save_path || "/downloads"}
        />
      )}
    </form>
  )
}
```

### Dashboard Speed Limits Display

```typescript
function DashboardSpeedLimits({ instanceId }: { instanceId: number }) {
  const { preferences } = useInstancePreferences(instanceId)
  const { data: stats } = useInstanceStats(instanceId)
  
  const formatSpeed = (speed: number) => speed === 0 ? "Unlimited" : `${speed} KB/s`
  
  return (
    <Card>
      <CardHeader>
        <CardTitle>Speed Limits</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 gap-4">
          <div>
            <p className="text-sm text-muted-foreground">Download</p>
            <p className="text-lg font-semibold">
              {formatSpeed(preferences?.dl_limit || 0)}
            </p>
            <p className="text-xs text-muted-foreground">
              Current: {formatSpeed(stats?.dlspeed || 0)}
            </p>
          </div>
          <div>
            <p className="text-sm text-muted-foreground">Upload</p>
            <p className="text-lg font-semibold">
              {formatSpeed(preferences?.up_limit || 0)}
            </p>
            <p className="text-xs text-muted-foreground">
              Current: {formatSpeed(stats?.upspeed || 0)}
            </p>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
```

### Global Preferences Context (Optional)

If preferences need to be available globally for the current instance:

```typescript
const InstancePreferencesContext = createContext<ReturnType<typeof useInstancePreferences> | null>(null)

export function InstancePreferencesProvider({ 
  instanceId, 
  children 
}: { 
  instanceId: number
  children: React.ReactNode 
}) {
  const preferencesData = useInstancePreferences(instanceId)
  
  return (
    <InstancePreferencesContext.Provider value={preferencesData}>
      {children}
    </InstancePreferencesContext.Provider>
  )
}

export function useCurrentInstancePreferences() {
  const context = useContext(InstancePreferencesContext)
  if (!context) {
    throw new Error("useCurrentInstancePreferences must be used within InstancePreferencesProvider")
  }
  return context
}
```

## Research Findings & Component Integration

### Existing Components That Will Benefit

Based on codebase analysis, these components already exist and should integrate with preferences:

1. **AddTorrentDialog** (`web/src/components/torrents/AddTorrentDialog.tsx`):
   - Currently has manual `startPaused`, `savePath`, `category` fields
   - Should use `start_paused_enabled`, `save_path`, `auto_tmm_enabled` as defaults

2. **Dashboard Stats** (various dashboard components):
   - Can display global `dl_limit`/`up_limit` alongside current speeds
   - Show queue status when `queueing_enabled` is true

3. **Settings Page** (`web/src/pages/Settings.tsx`):
   - Already has Account, API Keys, Appearance tabs
   - Perfect place to add "Instance Preferences" tab

4. **TorrentTable** components:
   - Can use queue settings for visual indicators
   - Show warnings when limits are reached

### Updated TypeScript Interface

The go-qbittorrent library has more fields than initially documented:

```typescript
export interface AppPreferences {
  // Core limits and speeds
  dl_limit: number
  up_limit: number
  alt_dl_limit: number
  alt_up_limit: number
  
  // Queue management  
  queueing_enabled: boolean
  max_active_downloads: number
  max_active_torrents: number
  max_active_uploads: number
  max_active_checking_torrents: number
  
  // Network settings
  listen_port: number
  random_port: boolean
  upnp: boolean
  
  // Connection limits
  max_connec: number
  max_connec_per_torrent: number
  max_uploads: number
  max_uploads_per_torrent: number
  
  // Seeding limits
  max_ratio_enabled: boolean
  max_ratio: number
  max_seeding_time_enabled: boolean
  max_seeding_time: number
  
  // Paths and file management
  save_path: string
  temp_path: string
  temp_path_enabled: boolean
  auto_tmm_enabled: boolean
  
  // Startup behavior
  start_paused_enabled: boolean
  
  // BitTorrent protocol
  dht: boolean
  pex: boolean
  lsd: boolean
  encryption: number
  anonymous_mode: boolean
  
  // Scheduler
  scheduler_enabled: boolean
  schedule_from_hour: number
  schedule_from_min: number
  schedule_to_hour: number
  schedule_to_min: number
  scheduler_days: number
  
  // Web UI (read-only reference)
  web_ui_port: number
  web_ui_username: string
  use_https: boolean
  
  // Additional fields from actual API
  add_trackers_enabled: boolean
  add_trackers: string
  announce_to_all_tiers: boolean
  announce_to_all_trackers: boolean
  // ... (100+ more fields available in go-qbittorrent library)
}
```

## Benefits

1. **Performance**: Cached at backend level (60s TTL) to avoid overwhelming qBittorrent
2. **Consistency**: Follows existing patterns for categories/tags caching
3. **Type Safety**: Full TypeScript support with proper interfaces
4. **Optimistic UI**: Immediate feedback when updating preferences
5. **Flexible**: Can be used per-component or globally via context
6. **Maintainable**: Clear separation between backend caching and frontend state
7. **Smart Defaults**: AddTorrentDialog becomes more user-friendly with instance defaults
8. **Enhanced Dashboard**: Real-time speed limits display alongside current usage

## Implementation Order

### Phase 1: Core Implementation ✅ COMPLETED
1. **Backend SyncManager methods** (`internal/qbittorrent/sync_manager.go`) ✅
   - ✅ Added `GetAppPreferences` method with 60s TTL caching
   - ✅ Added `SetAppPreferences` method with cache invalidation

2. **API handler and routes** (`internal/api/handlers/preferences.go`, `internal/api/router.go`) ✅  
   - ✅ Created PreferencesHandler with GET/PATCH endpoints
   - ✅ Added routes under `/instances/{instanceID}/preferences`

3. **Frontend API client methods** (`web/src/lib/api.ts`) ✅
   - ✅ Added `getInstancePreferences` method
   - ✅ Added `updateInstancePreferences` method with partial update support

4. **TypeScript types** (`web/src/types/index.ts`) ✅
   - ✅ Added comprehensive `AppPreferences` interface with all common fields

5. **React hook** (`web/src/hooks/useInstancePreferences.ts`) ✅
   - ✅ Implemented hook following existing patterns
   - ✅ Includes optimistic updates and error handling
   - ✅ Uses 5s stale time with 60s refetch interval

**Phase 1 Status**: ✅ **COMPLETED** (commit `7655c2a`)
- All backend API endpoints implemented and tested
- Frontend infrastructure complete with TypeScript support  
- Caching strategy matches existing patterns (60s TTL)
- Ready for Phase 2 component integration

### Phase 2: Component Integration ✅ COMPLETED
6. **Update useInstanceMetadata hook** (`web/src/hooks/useInstanceMetadata.ts`) ✅
   - ✅ Include preferences alongside categories/tags for efficiency
   - ✅ Reduce API calls by batching related data

7. **Enhance AddTorrentDialog** (`web/src/components/torrents/AddTorrentDialog.tsx`) ✅
   - ✅ Use preferences for smart defaults (save_path, start_paused_enabled, auto_tmm_enabled)
   - ✅ Show/hide savePath input based on auto_tmm setting
   - ✅ Display informative messages about Automatic Torrent Management status

8. **Add Dashboard speed limits display** ✅
   - ✅ Created DashboardSpeedLimits component showing global limits vs current speeds
   - ✅ Integrated into dashboard alongside existing stats
   - ✅ Per-instance speed limits display for connected instances

**Phase 2 Status**: ✅ **COMPLETED** (commit `2fa58c5`)
- Enhanced useInstanceMetadata hook batches preferences with categories/tags
- AddTorrentDialog now uses instance preferences for smart defaults and conditional UI
- Dashboard displays per-instance speed limits alongside current speeds
- All components integrate seamlessly with existing patterns
- Ready for Phase 3 settings integration

### Phase 3: Settings Integration ✅ COMPLETED
9. **Add Instance Preferences tab to Settings page** ✅
   - ✅ Created InstancePreferencesForm component with comprehensive preference management
   - ✅ Added Instance Preferences tab to Settings page alongside Security, Themes, and API Keys
   - ✅ Implemented form sections: Speed Limits (with sliders), Queue Management, File Management, Seeding Limits
   - ✅ Added instance selector dropdown for connected instances
   - ✅ Integrated with useInstancePreferences hook for optimistic updates
   - ✅ Added proper TypeScript typing and error handling

**Phase 3 Status**: ✅ **COMPLETED** (commit `21bd2d2`)
- Full settings integration with comprehensive preference management UI
- Instance-specific preferences with real-time updates
- User-friendly form controls with proper validation and feedback
- Seamless integration with existing Settings page patterns

### Phase 4: Move Instance Preferences to Dashboard Context ✅ COMPLETED
10. **Refactor Instance Preferences to Dialog-based Access** ✅
   - ✅ Created InstancePreferencesDialog component with shadcn Dialog wrapper
   - ✅ Modified InstancePreferencesForm to accept instanceId prop and hide instance selector when provided
   - ✅ Added Settings icon button to Dashboard InstanceCard components (all states: connected, disconnected, error)
   - ✅ Removed Instance Preferences tab from Settings page
   - ✅ Better UX with contextual access where instances are used

**Phase 4 Status**: ✅ **COMPLETED** (commit `c724ee7`)
- Full instance preferences dialog accessible from Dashboard instance cards
- Settings icon appears on connected instances and disconnected instances without errors
- Dialog shows comprehensive preference management with all sections
- Clean separation between app-wide settings and instance-specific preferences
- Ready for Phase 5 refinement

### Phase 5: Break Preferences into Focused Dialogs ✅ COMPLETED
11. **Create Focused Preference Dialogs** ✅
   - ✅ Replace single large preferences dialog with focused, smaller dialogs
   - ✅ Add dropdown menu to instance cards with specific options
   - ✅ Each menu item opens a targeted dialog for better UX

#### Implementation Steps:
1. **Create Individual Preference Components** ✅
   - ✅ SpeedLimitsForm component (speed limits section only)
   - ✅ QueueManagementForm component (queue settings section only)  
   - ✅ FileManagementForm component (paths and auto-management section only)
   - ✅ SeedingLimitsForm component (seeding ratio/time limits section only)

2. **Create Focused Dialog Components** ✅
   - ✅ SpeedLimitsDialog
   - ✅ QueueManagementDialog
   - ✅ FileManagementDialog  
   - ✅ SeedingLimitsDialog

3. **Add Dropdown Menu to Instance Cards** ✅
   - ✅ Replace single Settings button with dropdown menu (now uses MoreVertical icon)
   - ✅ Menu options: "Speed Limits", "Queue Management", "File Management", "Seeding Limits"
   - ✅ Each option opens the corresponding focused dialog
   - ✅ Created InstanceActionsDropdown component managing all dialog states

4. **Remove Large Preferences Dialog** ✅
   - ✅ Remove InstancePreferencesDialog component
   - ✅ Remove comprehensive InstancePreferencesForm
   - ✅ Keep the individual focused components

**Phase 5 Status**: ✅ **COMPLETED** (commit `e7d4bb4`)
- All focused dialogs implemented with individual preference forms
- Dashboard updated to use dropdown menu instead of single Settings button
- Improved UX with smaller, purpose-specific dialogs
- Better mobile experience and reduced cognitive load
- Clean separation of concerns with focused components

#### UX Benefits Achieved:
- **Smaller Dialogs**: Less overwhelming, easier to navigate
- **Faster Access**: Direct access to specific settings via dropdown menu
- **Mobile Friendly**: Smaller dialogs work better on mobile devices
- **Clear Purpose**: Each dialog has a specific, focused purpose
- **Reduced Cognitive Load**: Users see only relevant settings for their task

### Phase 6: Advanced Features ✅ COMPLETED
11. **Queue status indicators** ✅
   - ✅ Enhanced priority column with queue badges (Q1, Q2, etc.) and DL/UP indicators
   - ✅ Updated state column to show queue position alongside status
   - ✅ Made priority column visible by default
   - ✅ Added queue status to Dashboard InstanceCard (shows DL/UP/Total ratios when queueing enabled)
   - ✅ Enhanced TorrentDetailsPanel with queue priority and limits information

12. **Global preferences context assessment** ✅
   - ✅ Determined not needed - current useInstanceMetadata pattern is efficient and sufficient
   - ✅ Preferences already accessible in all components that need them

**Phase 6 Status**: ✅ **COMPLETED** (commit `5f848eb`)
- Full queue status visualization throughout the UI
- Priority column shows queue badges for queued torrents
- State column displays queue position (#1, #2, etc.)
- Dashboard instance cards show queue utilization ratios
- Torrent details panel includes comprehensive queue information
- Smart conditional display - only shows when queueing is enabled
- Maintains existing UI patterns and performance characteristics

## Testing Considerations

- Test cache invalidation after updates
- Verify optimistic updates rollback on error  
- Check preference persistence across page reloads
- Validate partial updates work correctly
- Test with multiple instances having different preferences
- **Integration testing**: Verify AddTorrentDialog uses correct defaults
- **UI testing**: Ensure speed limits display updates correctly
- **Cache testing**: Verify 60s TTL aligns with categories/tags patterns