# Intercepted qBittorrent API Endpoints

This document lists the qBittorrent Web API endpoints that are intercepted by qui's proxy and served from qui's sync manager instead of being forwarded to qBittorrent.

## Why Intercept?

These endpoints are intercepted to:
- Reduce load on qBittorrent instances
- Provide faster response times using cached/synchronized data
- Enable advanced features like search across large torrent lists
- Leverage qui's optimized data structures and filtering

## Intercepted Endpoints

### Read Operations (GET)

All intercepted read endpoints use qui's sync manager for faster response times and reduced qBittorrent load.

#### 1. Torrent List (Standard)
- **Endpoint**: `/api/v2/torrents/info`
- **SyncManager Method**: `GetTorrentsWithFilters`
- **Benefits**: 
  - Fast response using cached/synchronized data
  - Efficient filtering and sorting
  - Supports standard qBittorrent parameters (`hashes`, `filter`, `category`, `tag`, `sort`, `reverse`, `limit`, `offset`)
  - Full qBittorrent API compatibility
- **Note**: If an unknown query parameter is provided the proxy forwards the request upstream to maintain compatibility; does NOT support search parameter or advanced qui filtering

### 2. Torrent List (Enhanced)
- **Endpoint**: `/api/v2/torrents/search`
- **SyncManager Method**: `GetTorrentsWithFilters`
- **Benefits**: 
  - All benefits of `/api/v2/torrents/info` plus:
  - Fast fuzzy search across torrent names via `search` parameter
  - Advanced qui-specific filtering capabilities like `unregistered`, `tracker_down`
  - Returns full qui response with metadata (stats, counts, categories, tags, server state)
- **Note**: This is a qui-specific endpoint, not part of standard qBittorrent API

### 3. Categories
- **Endpoint**: `/api/v2/torrents/categories`
- **SyncManager Method**: `GetCategories`
- **Benefits**:
  - Instant response from synchronized category data
  - No additional API call to qBittorrent needed

### 4. Tags
- **Endpoint**: `/api/v2/torrents/tags`
- **SyncManager Method**: `GetTags`
- **Benefits**:
  - Instant response from synchronized tag data
  - Automatically sorted for consistent UI display

### 5. Torrent Properties
- **Endpoint**: `/api/v2/torrents/properties`
- **Query Parameter**: `hash` (required)
- **SyncManager Method**: `GetTorrentProperties`
- **Benefits**:
  - Reduced latency for frequently accessed torrent details

### 6. Torrent Trackers
- **Endpoint**: `/api/v2/torrents/trackers`
- **Query Parameter**: `hash` (required)
- **SyncManager Method**: `GetTorrentTrackers`
- **Benefits**:
  - Automatic tracker icon discovery and caching
  - Enhanced tracker status information

### 7. Torrent Peers
- **Endpoint**: `/api/v2/sync/torrentPeers`
- **Query Parameter**: `hash` (required)
- **SyncManager Method**: `GetTorrentPeers`
- **Benefits**:
  - Incremental peer updates
  - Efficient peer list synchronization

### 8. Torrent Files
- **Endpoint**: `/api/v2/torrents/files`
- **Query Parameter**: `hash` (required)
- **SyncManager Method**: `GetTorrentFiles`
- **Benefits**:
  - Fast file list retrieval
  - Consistent with qui's data synchronization

## Implementation Details

### Route Registration

The intercepted endpoints are registered as explicit chi routes in the proxy handler:

```go
// Read endpoints (served from qui's sync manager)
r.Get("/api/v2/torrents/info", h.handleTorrentsInfo)        // Standard qBittorrent API compatibility
r.Get("/api/v2/torrents/search", h.handleTorrentSearch)          // Qui-enhanced endpoint with search & advanced filtering
r.Get("/api/v2/torrents/categories", h.handleCategories)
r.Get("/api/v2/torrents/tags", h.handleTags)
r.Get("/api/v2/torrents/properties", h.handleTorrentProperties)
r.Get("/api/v2/torrents/trackers", h.handleTorrentTrackers)
r.Get("/api/v2/sync/torrentPeers", h.handleTorrentPeers)
r.Get("/api/v2/torrents/files", h.handleTorrentFiles)
```

### Middleware Stack

Each intercepted endpoint passes through:
1. **ClientAPIKeyMiddleware** - Validates the client API key
2. **prepareProxyContextMiddleware** - Prepares instance context and credentials
3. **Handler Function** - Calls appropriate SyncManager method

### Response Modification

For `/api/v2/auth/login` requests that are proxied to qBittorrent:
- If the login is successful but qBittorrent doesn't set a cookie
- The `modifyAuthLoginResponse` function injects a session cookie
- This ensures compatibility with clients that expect cookie-based authentication

### Response Format

All intercepted endpoints return responses in qBittorrent's native JSON format, ensuring full compatibility with existing qBittorrent clients.

### Write Operations (POST)

Some write operations are intercepted to perform additional qui-specific actions (like cache invalidation) before forwarding to qBittorrent:

#### 1. Set Location
- **Endpoint**: `/api/v2/torrents/setLocation`
- **Action**: Invalidates file cache for affected torrents after forwarding to qBittorrent
- **Reason**: File paths may change when torrent location changes

#### 2. Rename File
- **Endpoint**: `/api/v2/torrents/renameFile`
- **Action**: Invalidates file cache for the torrent after forwarding to qBittorrent
- **Reason**: Cached file names need to be refreshed

#### 3. Rename Folder
- **Endpoint**: `/api/v2/torrents/renameFolder`
- **Action**: Invalidates file cache for the torrent after forwarding to qBittorrent
- **Reason**: All file paths under the folder change

#### 4. Delete Torrents
- **Endpoint**: `/api/v2/torrents/delete`
- **Action**: Invalidates file cache for deleted torrents after forwarding to qBittorrent
- **Reason**: Cleanup cached data for removed torrents

#### 5. Reannounce torrents
- **Endpoint**: `/api/v2/torrents/reannounce`
- **Action**: When tracker monitoring is enabled for an instance, qui consumes the request and delegates it to the internal reannounce service with debouncing. Hashes that aren't monitored are still forwarded upstream.
- **Reason**: Prevents duplicate or excessive reannounce calls by centralizing retry logic.

## Non-Intercepted Endpoints

All other endpoints are forwarded to qBittorrent via reverse proxy, including:
- Other write operations (add torrents, pause, resume, set category/tags, etc.)
- Application settings
- Transfer info
- RSS feeds
- Search plugins
- Authentication logout and other auth endpoints

## Future Considerations

Potential endpoints that could be intercepted in the future:
- `/api/v2/sync/maindata` - Could provide qui's enhanced sync data with additional metadata
- `/api/v2/transfer/info` - Could aggregate transfer statistics across instances
