# Async Indexer Filtering

This document explains the async indexer filtering capabilities in the cross-seed service.

## Overview

The async filtering system provides immediate UI updates with capability filtering results while content filtering continues in the background. This significantly improves performance and responsiveness, especially when dealing with large torrent libraries.

All filtering operations now use the async approach by default - there is no synchronous fallback as the async method provides better performance in all scenarios.

## Key Components

### AsyncIndexerFilteringState

Contains the state of async filtering operations:

```go
type AsyncIndexerFilteringState struct {
    CapabilitiesCompleted bool              // True when capability filtering is done
    ContentCompleted      bool              // True when content filtering is done
    CapabilityIndexers    []int             // Indexers after capability filtering
    FilteredIndexers      []int             // Final filtered indexers
    ExcludedIndexers      map[int]string    // Excluded indexers with reasons
    ContentMatches        []string          // Existing content matches found
    Error                 string            // Any error that occurred
}
```

### AsyncTorrentAnalysis

Combines torrent info with filtering state:

```go
type AsyncTorrentAnalysis struct {
    TorrentInfo    *TorrentInfo
    FilteringState *AsyncIndexerFilteringState
}
```

## API Methods

### Primary Methods (Async-First)

#### `AnalyzeTorrentForSearchAsync()`
- **Purpose**: Performs analysis with async content filtering
- **Returns**: Immediate capability-filtered results + background content filtering
- **Use Case**: Interactive UI operations requiring immediate response

#### `AnalyzeTorrentForSearch()`
- **Purpose**: Analyzes torrent with immediate capability results
- **Returns**: TorrentInfo with capability-filtered indexers
- **Use Case**: When you need immediate results and don't want to manage async state
- **Note**: Now uses async internally but returns immediate capability results

#### `SearchTorrentMatches()`
- **Purpose**: Searches for matching torrents using async filtering
- **Returns**: Search results using capability-filtered indexers immediately
- **Use Case**: Interactive search operations

### Automation Methods

#### `processSearchCandidate()` (Automation)
- **Purpose**: Processes torrents in search automation runs
- **Performance**: Now uses async filtering for better automation performance
- **Use Case**: Background automation that benefits from faster capability filtering

## Usage Examples

### For Interactive UI

```go
// Get immediate capability filtering results for UI
asyncResult, err := service.AnalyzeTorrentForSearchAsync(ctx, instanceID, hash, true)
if err != nil {
    return err
}

// Use capability-filtered indexers immediately
if asyncResult.FilteringState.CapabilitiesCompleted {
    updateIndexerUI(asyncResult.FilteringState.CapabilityIndexers)
}

// Optional: Poll for content filtering completion
go func() {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        if asyncResult.FilteringState.ContentCompleted {
            updateIndexerUI(asyncResult.FilteringState.FilteredIndexers)
            updateExcludedIndexers(asyncResult.FilteringState.ExcludedIndexers)
            updateContentMatches(asyncResult.FilteringState.ContentMatches)
            break
        }
    }
}()
```

### For Simple Operations

```go
// Get immediate capability results without managing async state
torrentInfo, err := service.AnalyzeTorrentForSearch(ctx, instanceID, hash)
if err != nil {
    return err
}
// Use torrentInfo.FilteredIndexers immediately (capability-filtered)
```

### For Search Operations

```go
// SearchTorrentMatches automatically uses async filtering
response, err := service.SearchTorrentMatches(ctx, instanceID, hash, opts)
// This uses capability-filtered indexers immediately for search
// Content filtering continues in background but doesn't block the search
```

## Performance Benefits

1. **Immediate Response**: Capability filtering (~1 second) provides immediate indexer list
2. **Non-blocking**: Content filtering (~5-30 seconds) runs in background without blocking operations
3. **Better Automation**: Search automation now processes torrents faster with async filtering
4. **Reduced Timeouts**: No more 30-second waits for content filtering in interactive operations
5. **Progressive Enhancement**: Basic filtering first, refined filtering as background task completes

## Implementation Details

### Async Processing Flow

1. **Phase 1 (Immediate)**: Capability filtering
   - Validates indexer capabilities against torrent requirements
   - Returns results in ~1 second
   - Suitable for immediate UI updates

2. **Phase 2 (Background)**: Content filtering
   - Scans existing torrents to avoid redundant content
   - Runs asynchronously in background goroutine
   - Updates state when complete (5-30 seconds)
   - UI can poll for refined results

### Thread Safety

- `AsyncIndexerFilteringState` uses atomic-like updates in background goroutine
- State completion flags are set last to ensure data consistency
- Callers should treat returned state as read-only

### Error Handling

- Capability filtering errors fall back to using all provided indexers
- Content filtering errors fall back to capability-filtered results
- All errors are captured in the `Error` field for debugging

## Migration Notes

### What Changed

- **Removed**: Synchronous `filterIndexerIDsForTorrent()` method
- **Updated**: `AnalyzeTorrentForSearch()` now returns immediate capability results
- **Updated**: `processSearchCandidate()` uses async filtering for automation
- **Updated**: All operations prioritize immediate capability filtering over waiting for content filtering

### Benefits of New Approach

1. **No Blocking**: Interactive operations never wait for slow content filtering
2. **Better Automation**: Search automation processes torrents faster
3. **Consistent Performance**: All operations use the same high-performance async approach
4. **Simplified API**: No need to choose between sync/async - async is always used

### UI Integration

```go
// Before: Could take 10+ seconds
torrentInfo, err := service.AnalyzeTorrentForSearch(ctx, instanceID, hash)
// Now: Returns in ~1 second with capability-filtered indexers

// For advanced UI that wants refined results:
asyncResult, err := service.AnalyzeTorrentForSearchAsync(ctx, instanceID, hash, true)
// Immediate capability results + optional polling for refined results
```

## Future Enhancements

1. **WebSocket Integration**: Real-time updates when content filtering completes
2. **State Persistence**: Store async state for polling across HTTP requests  
3. **Batch Processing**: Process multiple torrents' content filtering in parallel
4. **Smart Caching**: Cache content filtering results to avoid redundant processing