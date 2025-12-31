# Orphan Scan

Finds and removes files in your download directories that aren't associated with any torrent.

## How It Works

1. **Scan roots are determined dynamically** - qui scans all unique `SavePath` directories from your current torrents, not qBittorrent's default download directory
2. Files not referenced by any torrent are flagged as orphans
3. You preview the list before confirming deletion
4. Empty directories are cleaned up after file deletion

## Important: Abandoned Directories

Directories are only scanned if at least one torrent points to them. If you delete all torrents from a directory, that directory is no longer a scan root and any leftover files there won't be detected.

Example: You have torrents in `/downloads/old-stuff/`. You delete all those torrents. Orphan scan no longer knows about `/downloads/old-stuff/` and won't clean it up.

## Settings

| Setting | Description |
|---------|-------------|
| **Grace period** | Skip files modified within this window (default: 10 minutes) |
| **Ignore paths** | Directories to exclude from scanning |
| **Scan interval** | How often scheduled scans run (default: 24 hours) |
| **Max files per run** | Limit results to prevent overwhelming large scans (default: 10,000) |

## Workflow

1. Trigger a scan (manual or scheduled)
2. Review the preview list of orphan files
3. Confirm deletion
4. Files are deleted and empty directories cleaned up
