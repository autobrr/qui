# qui

A fast, modern web interface for qBittorrent. Supports managing multiple qBittorrent instances from a single, lightweight application.

<div align="center">
  <img src=".github/assets/qui.png" alt="qui" width="100%" />
</div>

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Docker](#docker)
- [Updating](#updating)
- [Configuration](#configuration)
  - [Base URL](#base-url-configuration)
  - [OpenID Connect (OIDC)](#openid-connect-oidc)
  - [CLI Commands](#cli-commands)
- [Backups & Restore Modes](#backups--restore-modes)
- [Cross-Seed](#cross-seed)
- [Reverse Proxy for External Applications](#reverse-proxy-for-external-applications)
- [External Programs](#external-programs)
- [Tracker Icons](#tracker-icons)
- [API](#api)
- [Metrics](#metrics)
- [qBittorrent Version Compatibility](#qbittorrent-version-compatibility)
- [Community](#community)
- [Support Development](#support-development)
- [Contributing](#contributing)
- [License](#license)

## Features

- **Single Binary**: No dependencies, just download and run
- **Multi-Instance Support**: Manage all your qBittorrent instances from one place
- **Fast & Responsive**: Optimized for performance with large torrent collections
- **Clean Interface**: Modern UI built with React and shadcn/ui components
- **Multiple Themes**: Choose from various color themes
- **Base URL Support**: Serve from a subdirectory (e.g., `/qui/`) for reverse proxy setups
- **OIDC Single Sign-On**: Authenticate through your OpenID Connect provider
- **External Programs**: Launch custom scripts from the torrent context menu ([guide](internal/api/handlers/EXTERNAL_PROGRAMS.md))
- **Tracker Reannounce**: Automatically fix stalled torrents when qBittorrent doesn't retry fast enough ([info](internal/services/reannounce/REANNOUNCE.md))
- **Tracker Rules**: Apply per-tracker speed limits, ratio caps, and seeding time limits automatically ([info](internal/services/trackerrules/TRACKER_RULES.md))
- **Backups & Restore**: Scheduled snapshots with incremental, overwrite, and complete restore modes ([info](#backups--restore-modes))
- **Cross-Seed**: Automatically find and add matching torrents across trackers with autobrr webhook integration ([info](#cross-seed))
- **Reverse Proxy**: Transparent qBittorrent proxy for external apps like autobrr, Sonarr, and Radarr—no credential sharing needed ([info](#reverse-proxy-for-external-applications))

## Installation

### Quick Install (Linux x86_64)

```bash
# Download and extract the latest release
wget $(curl -s https://api.github.com/repos/autobrr/qui/releases/latest | grep browser_download_url | grep linux_x86_64 | cut -d\" -f4)
```

### Unpack

Run with root or sudo. If you do not have root, or are on a shared system, place the binaries somewhere in your home directory like ~/.bin.

```bash
tar -C /usr/local/bin -xzf qui*.tar.gz
```

This will extract both qui to /usr/local/bin. Note: If the command fails, prefix it with sudo and re-run again.

### Manual Download

Download the latest release for your platform from the [releases page](https://github.com/autobrr/qui/releases).

### Run

```bash
# Make it executable (Linux/macOS)
chmod +x qui

# Run
./qui serve
```

The web interface will be available at http://localhost:7476

### Seedbox installers

Bytesized installer (NOT TESTED)

```bash
wget -O installer.sh https://get.autobrr.com/qui/bytesized && chmod +x installer.sh && ./installer.sh
```

Feral hosting installer

```bash
wget -O installer.sh https://get.autobrr.com/qui/feral && chmod +x installer.sh && ./installer.sh
```

HostingByDesign App slots installer

```bash
wget -O installer.sh https://get.autobrr.com/qui/hostingbydesign && chmod +x installer.sh && ./installer.sh
```

Seedhost installer

```bash
wget -O installer.sh https://get.autobrr.com/qui/seedhost && chmod +x installer.sh && ./installer.sh
```

Ultra.cc installer

```bash
wget -O installer.sh https://get.autobrr.com/qui/ultra && chmod +x installer.sh && ./installer.sh
```

Whatbox installer

```bash
wget -O installer.sh https://get.autobrr.com/qui/whatbox && chmod +x installer.sh && ./installer.sh
```


### First Setup

1. Open your browser to http://localhost:7476
2. Create your admin account
3. Add your qBittorrent instance(s)
4. Start managing your torrents

## Docker

```bash
# Using Docker Compose
docker compose up -d

# Or standalone
docker run -d \
  -p 7476:7476 \
  -v $(pwd)/config:/config \
  ghcr.io/autobrr/qui:latest
```

### Unraid

Our release workflow builds multi-architecture images (`linux/amd64`, `linux/arm64`, and friends) and publishes them to `ghcr.io/autobrr/qui`, so the container should work on Unraid out of the box.

**Deploy from the Docker tab**
- Open **Docker → Add Container**
- Set **Name** to `qui`
- Set **Repository** to `ghcr.io/autobrr/qui:latest`
- Keep the default **Network Type** (`bridge` works for most setups)
- Add a port mapping: **Host port** `7476` → **Container port** `7476`
- Add a path mapping: **Container Path** `/config` → **Host Path** `/mnt/user/appdata/qui`
- (Optional) add environment variables for advanced settings (e.g., `QUI__BASE_URL`, `QUI__LOG_LEVEL`, `TZ`)
- Click **Apply** to pull the image and start the container

The `/config` mount stores `config.toml`, the SQLite database, and logs. Point it at your preferred appdata share so settings persist across upgrades.

If the app logs to stdout, check logs via Docker → qui → Logs; if it writes to files, they'll be under /config.

**Updating**
- Use Unraid's **Check for Updates** action to pull a newer `latest` image
- If you pinned a specific version tag, edit the repository field to the new tag when you're ready to upgrade
- Restart the container if needed after the image update so the new binary is loaded

## Updating

qui includes a built-in update command that automatically downloads and installs the latest release:

```bash
./qui update
```

**Note:** This feature is for standalone binary installations only. Docker users should pull the latest image instead:

```bash
docker compose pull && docker compose up -d
```

## Configuration

Configuration is stored in `config.toml` (created automatically on first run, or manually with `qui generate-config`). You can also use environment variables:

```bash
# Server
QUI__HOST=0.0.0.0        # Listen address
QUI__PORT=7476           # Port number
QUI__BASE_URL=/qui/      # Optional: serve from subdirectory

# Security
QUI__SESSION_SECRET_FILE=...  # Path to file containing secret. Takes precedence over QUI__SESSION_SECRET
QUI__SESSION_SECRET=...       # Auto-generated if not set

# Logging
QUI__LOG_LEVEL=INFO      # Options: ERROR, DEBUG, INFO, WARN, TRACE
QUI__LOG_PATH=...        # Optional: log file path
QUI__LOG_MAX_SIZE=50     # Optional: rotate when log file exceeds N megabytes (default: 50)
QUI__LOG_MAX_BACKUPS=3   # Optional: retain N rotated files (default: 3, 0 keeps all)

# Storage
QUI__DATA_DIR=...        # Optional: custom data directory (default: next to config)

# Tracker Icons
QUI__TRACKER_ICONS_FETCH_ENABLED=false  # Optional: set to false to disable remote tracker icon fetching (default: true)

# Metrics
QUI__METRICS_ENABLED=true   # Optional: enable Prometheus metrics (default: false)
QUI__METRICS_HOST=127.0.0.1  # Optional: metrics server bind address (default: 127.0.0.1)
QUI__METRICS_PORT=9074       # Optional: metrics server port (default: 9074)
QUI__METRICS_BASIC_AUTH_USERS=user:hash  # Optional: basic auth for metrics (bcrypt hashed)

# External Programs
# Configure the allow list from `config.toml`; there is no environment override to keep it read-only from the UI.
```

When `logPath` is set the server writes to disk using size-based rotation. Adjust `logMaxSize` and `logMaxBackups` in `config.toml` or the corresponding environment variables shown above to control the rotation thresholds and retention.

### Base URL Configuration

If you need to serve qui from a subdirectory (e.g., `https://example.com/qui/`), you can configure the base URL:

**Using Environment Variable**
```bash
QUI__BASE_URL=/qui/ ./qui
```

**Using Configuration File**

Edit your `config.toml`:
```toml
baseUrl = "/qui/"
```

**With Nginx Reverse Proxy**
```nginx
# Redirect /qui to /qui/ for proper SPA routing
location = /qui {
    return 301 /qui/;
}

location /qui/ {
    proxy_pass http://localhost:7476/qui/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

### OpenID Connect (OIDC)

Set `QUI__OIDC_ENABLED=true` to hand authentication off to an external identity provider. The built-in login screen automatically offers an "Sign in with OIDC" button when the backend detects a valid OIDC configuration.

- `QUI__OIDC_ISSUER` - Issuer URL from your IdP (e.g. `https://auth.example.com/realms/main`)
- `QUI__OIDC_CLIENT_ID` - Client ID registered for qui
- `QUI__OIDC_CLIENT_SECRET` - Client secret generated by the provider
- `QUI__OIDC_REDIRECT_URL` - Must match the redirect URI allowed by the provider. For a default install use `http://localhost:7476/api/auth/oidc/callback`. Include your base URL when reverse proxying (e.g. `https://host/qui/api/auth/oidc/callback`).
- `QUI__OIDC_DISABLE_BUILT_IN_LOGIN` - Set to `true` to hide the local username/password form when OIDC is enabled

Example configuration via environment variables:

```bash
QUI__OIDC_ENABLED=true \
QUI__OIDC_ISSUER=https://auth.example.com/realms/main \
QUI__OIDC_CLIENT_ID=qui \
QUI__OIDC_CLIENT_SECRET=super-secret-value \
QUI__OIDC_REDIRECT_URL=https://qui.example.com/api/auth/oidc/callback \
QUI__OIDC_DISABLE_BUILT_IN_LOGIN=true
```

You can set the same options in `config.toml` using the `oidc*` keys generated by `qui generate-config`.

### CLI Commands

#### Generate Configuration File

Create a default configuration file without starting the server:

```bash
# Generate config in OS-specific default location
./qui generate-config

# Generate config in custom directory
./qui generate-config --config-dir /path/to/config/

# Generate config with custom filename
./qui generate-config --config-dir /path/to/myconfig.toml
```

#### User Management

Create and manage user accounts from the command line:

```bash
# Create initial user account
./qui create-user --username admin --password mypassword

# Create user with prompts (secure password input)
./qui create-user --username admin

# Change password for existing user (no old password required)
./qui change-password --username admin --new-password mynewpassword

# Change password with secure prompt
./qui change-password --username admin

# Pipe passwords for scripting (works with both commands)
echo "mypassword" | ./qui create-user --username admin
echo "newpassword" | ./qui change-password --username admin
printf "password" | ./qui change-password --username admin
./qui change-password --username admin < password.txt

# All commands support custom config/data directories
./qui create-user --config-dir /path/to/config/ --username admin
```

**Notes:**
- Only one user account is allowed in the system
- Passwords must be at least 8 characters long
- Interactive prompts use secure input (passwords are masked)
- Supports piped input for automation and scripting
- Commands will create the database if it doesn't exist
- No password confirmation required - perfect for automation

**Default locations:**
- Linux/macOS: `~/.config/qui/config.toml`
- Windows: `%APPDATA%\qui\config.toml`

#### Update Command

Keep your qui installation up-to-date:

```bash
# Update to the latest version
./qui update
```

#### Command Line Flags

```bash
# Specify config directory (config.toml will be created inside)
./qui serve --config-dir /path/to/config/

# Specify data directory for database and other data files
./qui serve --data-dir /path/to/data/
```

## Backups & Restore Modes

qui can take scheduled or ad-hoc snapshots of a qBittorrent instance. Each snapshot includes the torrent archive, tags, categories (with save paths), and cached `.torrent` blobs so that you can recreate the original state later.

Once backups are enabled for an instance the backlog UI exposes a **Restore** action for each run. Restores support three distinct modes:

- Incremental – safest option. Creates any categories, tags, or torrents that are missing from the live instance but never modifies or removes existing data. Use this when you just want to seed new items into an active qBittorrent without touching what is already there.
- Overwrite – performs the incremental work **and** updates existing resources to match the snapshot (e.g. adjusts category save paths or rewrites per-torrent categories/tags). It still refuses to delete anything. This works well when your live instance has drifted but you do not want to prune it.
- Complete – full reconciliation. Runs the overwrite steps and then deletes categories, tags, and torrents that are not present in the snapshot. This is ideal when you need to roll an instance back to an earlier point in time, but it should only be used when you are certain the snapshot is authoritative.

Every restore begins with a dry-run preview so you can inspect planned changes. Unsupported differences (such as mismatched infohashes or file sizes) are surfaced as warnings; they require manual follow-up regardless of mode.

### Importing Backups

Downloaded backups can be imported into any qui instance. Useful for migrating to a new server or recovering after data loss. Click **Import** on the Backups page and select the backup file. All export formats are supported.

## Cross-Seed

qui includes intelligent cross-seeding capabilities that help you automatically find and add matching torrents across different trackers. This allows you to seed the same content on multiple trackers.

> [!NOTE]
> qui adds cross-seeded torrents by inheriting the **Automatic Torrent Management (AutoTMM)** state from the matched torrent. If the matched torrent uses AutoTMM, the cross-seed will too; if the matched torrent has a custom save path (AutoTMM disabled), the cross-seed will use the same explicit path. This reuses existing files directly without creating hardlinks.
>
> For detailed information about category behavior, save paths, and best practices, see the [Cross-Seeding Guide](docs/CROSS_SEEDING.md).

### Prerequisites

You need Prowlarr or Jackett to provide Torznab indexer feeds. Add your indexers in **Settings → Indexers** using the "1-click sync" feature to import from Prowlarr/Jackett automatically.

### Discovery Methods

qui offers several ways to find cross-seed opportunities:

#### RSS Automation

Scheduled polling of tracker RSS feeds. Configure in the **Automation** tab on the Cross-Seed page.

- **Run interval** - How often to poll feeds (minimum 30 minutes)
- **Target instances** - Which qBittorrent instances receive cross-seeds
- **Target indexers** - Limit to specific indexers or use all enabled ones

RSS automation processes the full feed from every enabled indexer on each run, matching against torrents across your target instances.

#### Seeded Torrent Search

Deep scan of torrents you already seed to find cross-seed opportunities on other trackers. Configure in the **Seeded search** tab.

- **Source instance** - The qBittorrent instance to scan
- **Categories/Tags** - Filter which torrents to include
- **Interval** - Delay between processing each torrent (minimum 60 seconds)
- **Cooldown** - Skip torrents searched within this window (minimum 12 hours)

> [!WARNING]
> Run sparingly. This deep scan touches every matching torrent and queries indexers for each one. Use RSS automation or autobrr for routine coverage; reserve seeded search for occasional catch-up passes.

#### Auto-Search on Completion

Triggers a cross-seed search when torrents finish downloading. Configure in the **Automation** tab under "Auto-search on completion".

- **Categories/Tags** - Filter which completed torrents trigger searches
- **Exclude categories/tags** - Skip torrents matching these filters

#### Manual Search

Right-click any torrent in the list to access cross-seed actions:

- **Search Cross-Seeds** - Query indexers for matching torrents on other trackers
- **Filter Cross-Seeds** - Show torrents in your library that share content with the selected torrent (useful for identifying existing cross-seeds)

### How qui Differs from cross-seed

qui takes a different approach than the [cross-seed](https://github.com/cross-seed/cross-seed) project:

| Aspect | cross-seed | qui |
|--------|-----------|-----|
| **File handling** | Creates hardlinks/symlinks to a separate directory | Reuses existing files directly |
| **AutoTMM** | Disabled (uses explicit save paths) | Inherits from matched torrent (unless "Use indexer name as category" is enabled) |
| **Category** | Uses dedicated `linkCategory` (e.g., "cross-seed-link") | Uses matched torrent's category with `.cross` suffix (configurable) |

### Global Settings

Configure matching behavior in the **Global rules** tab on the Cross-Seed page.

#### Matching

- **Find individual episodes** - When enabled, season packs also match individual episodes. When disabled, season packs only match other season packs. Episodes are added with AutoTMM disabled to prevent save path conflicts.
- **Size mismatch tolerance** - Maximum size difference percentage (default: 5%). Also determines auto-resume threshold after recheck.

#### Categories

- **Add .cross category suffix** (default: enabled) - Appends `.cross` to cross-seed categories (e.g., `movies` → `movies.cross`). This prevents Sonarr/Radarr from importing cross-seeded files as duplicates, since *arr apps typically monitor specific categories. Disable this for full Automatic Torrent Management (AutoTMM) support where cross-seeds should use identical categories and save paths as the source torrent.
- **Use indexer name as category** - Set the qBittorrent category to the indexer name instead of inheriting from the matched torrent. Uses explicit save paths, so AutoTMM is always disabled for these cross-seeds regardless of the source torrent's AutoTMM state (see [how qui differs from cross-seed](#how-qui-differs-from-cross-seed)). Mutually exclusive with `.cross` suffix.

#### Source Tagging

Configure tags applied to cross-seed torrents based on how they were discovered:

- **RSS Automation Tags** - Torrents added via RSS feed polling (default: `["cross-seed"]`)
- **Seeded Search Tags** - Torrents added via seeded torrent search (default: `["cross-seed"]`)
- **Completion Search Tags** - Torrents added via completion-triggered search (default: `["cross-seed"]`)
- **Webhook Tags** - Torrents added via `/apply` webhook (default: `["cross-seed"]`)
- **Inherit source torrent tags** - Also copy tags from the matched source torrent

#### Ignore Patterns

File patterns to skip when comparing torrents. Useful for excluding sidecar files like `.nfo`, `.srr`, or sample folders.

- Plain strings match any path ending in the text (e.g., `.nfo` ignores all `.nfo` files)
- Glob patterns treat `/` as a folder separator (e.g., `*/sample/*` ignores sample folders)

#### External Program

Optionally run an external program after successfully injecting a cross-seed torrent.

### When Rechecks Are Required

Most cross-seeds are added with hash verification skipped (`skip_checking=true`) and resume immediately. Some scenarios require a recheck:

#### 1. Name or folder alignment needed

When the cross-seed torrent has a different display name or root folder, qui renames them to match. qBittorrent must recheck to verify files at the new paths.

#### 2. Extra files in source torrent

When the source torrent contains files not on disk (NFO, SRT, samples not filtered by ignore patterns), a recheck determines actual progress.

#### Auto-resume behavior

- Default tolerance 5% → auto-resumes at ≥95% completion
- Torrents below threshold stay paused for manual investigation
- Configure via **Size mismatch tolerance** in Global rules

### autobrr Integration

qui integrates with autobrr through webhook endpoints, enabling real-time cross-seed detection when autobrr announces new releases.

#### How It Works

1. autobrr sees a new release from a tracker
2. autobrr sends the torrent name to qui's `/api/cross-seed/webhook/check` endpoint
3. qui searches your qBittorrent instances for matching content
4. qui responds with:
   - `200 OK` – matching torrent is complete and ready to cross-seed
   - `202 Accepted` – matching torrent exists but still downloading; retry later
   - `404 Not Found` – no matching torrent exists
5. On `200 OK`, autobrr sends the torrent file to `/api/cross-seed/apply`

#### Setup

**1. Create an API Key in qui**
- Go to **Settings → API Keys**
- Click **Create API Key**
- Name it (e.g., "autobrr webhook")
- Copy the generated key

**2. Configure autobrr External Filter**

In your autobrr filter, go to **External** tab → **Add new**:

| Field | Value |
|-------|-------|
| Type | `Webhook` |
| Name | `qui` |
| On Error | `Reject` |
| Endpoint | `http://localhost:7476/api/cross-seed/webhook/check` |
| HTTP Method | `POST` |
| HTTP Request Headers | `X-API-Key=YOUR_QUI_API_KEY` |
| Expected HTTP Status Code | `200` |

**Data (JSON):**
```json
{
  "torrentName": "{{ .TorrentName }}",
  "instanceIds": [1]
}
```

To search all instances, omit `instanceIds`:
```json
{
  "torrentName": "{{ .TorrentName }}"
}
```

**Field descriptions:**
- `torrentName` (required): The release name as announced
- `instanceIds` (optional): qBittorrent instance IDs to scan. Omit to search all instances.
- `findIndividualEpisodes` (optional): Override the global episode matching setting

**3. Configure Retry Handling**

Use autobrr's **Retry** block to handle `202 Accepted` responses:

- **Retry HTTP status code(s):** `202`
- **Maximum retry attempts:** `10`
- **Retry delay in seconds:** `4`

#### Apply Endpoint

When `/check` returns `200 OK`, send the torrent to `/api/cross-seed/apply`:

**Action setup in autobrr:**

| Field | Value |
|-------|-------|
| Action Type | `Webhook` |
| Name | `qui cross-seed` |
| Endpoint | `http://localhost:7476/api/cross-seed/apply?apikey=YOUR_QUI_API_KEY` |

**Payload (JSON):**
```json
{
  "torrentData": "{{ .TorrentDataRawBytes | toString | b64enc }}",
  "instanceIds": [1]
}
```

- `torrentData` - Base64-encoded torrent file bytes
- `instanceIds` - Target instances (omit to apply to any matching instance)
- `tags` (optional) - Override webhook tags from settings
- `category` (optional) - Override category (defaults to matched torrent's category)

Cross-seeded torrents are added paused with `skip_checking=true`. qui polls the torrent state and auto-resumes if progress meets the size tolerance threshold. If progress is too low, it remains paused for manual review.

#### Webhook Source Filters

By default, the webhook endpoint scans **all** torrents on your instances when looking for matches. You can configure filters to exclude certain categories or tags from being matched:

- **Exclude Categories:** Skip torrents in specific categories (e.g., `cross-seed-link`)
- **Exclude Tags:** Skip torrents with specific tags (e.g., `no-cross-seed`)
- **Include Categories:** Only match against torrents in these categories (leave empty for all)
- **Include Tags:** Only match against torrents with these tags (leave empty for all)

This is useful when:
- You have a legacy cross-seed category that shouldn't be re-matched
- Certain content types should never be considered for cross-seeding
- You want to exclude torrents with specific metadata tags

**Note:** Exclude filters take precedence over include filters. Tag matching is case-sensitive. When both category and tag include filters are configured, a torrent must pass both filter checks (matching at least one allowed category AND at least one allowed tag).

Configure in qui UI: **Cross-Seed → Global → Webhook Source Filters**

### Troubleshooting

#### Why didn't my cross-seed get added?

**Rate limiting (HTTP 429):** Indexers limit how frequently you can make requests. If you see errors like `"indexer TorrentLeech rate-limited until..."`, qui has recorded the cooldown and will skip that indexer until it's available. Check the **Scheduler Activity** panel on the Indexers page to see which indexers are in cooldown and when they'll be ready.

**Release didn't match:** qui uses strict matching to ensure cross-seeds have identical files. Both releases must match on:
- Title, year, and release group
- Resolution (1080p, 2160p)
- Source (WEB-DL, BluRay) and collection (AMZN, NF)
- Codec (x264, x265) and HDR format
- Audio format and channels
- Language, edition, cut, and version (v2, v3)
- Variants like IMAX, HYBRID, REPACK, PROPER

**Season pack vs episodes:** By default, season packs only match other season packs. Enable **Find individual episodes** in settings to allow season packs to match individual episode releases.

#### How do I see why a release was filtered?

Enable trace logging to see detailed rejection reasons:

```toml
loglevel = 'TRACE'
```

Look for `[CROSSSEED-MATCH] Release filtered` entries showing exactly which field caused the mismatch (e.g., `group_mismatch`, `resolution_mismatch`, `language_mismatch`).

## Reverse Proxy for External Applications

qui includes a built-in reverse proxy that allows external applications like autobrr, Sonarr, Radarr, and other tools to connect to your qBittorrent instances **without needing qBittorrent credentials**. qui handles authentication transparently, making integration seamless.

### How It Works

The reverse proxy feature:
- **Handles authentication automatically** - qui manages the qBittorrent login using your configured credentials
- **Isolates clients** - Each client gets its own API key
- **Provides transparent access** - Clients see qui as if it were qBittorrent directly
- **Reduces login thrash** - qui maintains a shared cookie jar and session, so your automation tools stop racing to re-authenticate against qBittorrent. That means fewer failed logins, less load on qBittorrent, and faster announce races because downstream apps reuse the live session instead of waiting for new tokens.

For a detailed list of the qBittorrent API endpoints served directly from qui's proxy (rather than forwarded upstream), see [internal/proxy/INTERCEPTED_ENDPOINTS.md](internal/proxy/INTERCEPTED_ENDPOINTS.md).

### Setup Instructions

#### 1. Create a Client Proxy API Key

1. Open qui in your browser
2. Go to **Settings → Client Proxy Keys**
3. Click **"Create Client API Key"**
4. Enter a name for the client (e.g., "Sonarr")
5. Choose the qBittorrent instance you want to proxy
6. Click **"Create Client API Key"**
7. **Copy the generated proxy url immediately** - it's only shown once

#### 2. Configure Your External Application

Use qui as the qBittorrent host with the special proxy URL format:

**Complete URL example:**
```
http://localhost:7476/proxy/abc123def456ghi789jkl012mno345pqr678stu901vwx234yz
```

#### Application-Specifics

**Sonarr / Radarr**
- Go to `Settings → Download Clients`
- Select `Show Advanced`
- Add a new **qBittorrent** client
- Set the host and port of qui
- Add URL Base (`/proxy/...`) - remember to include /qui/ if you use custom baseurl
- Click **Test** and then **Save** once the test succeeds

**autobrr**
- Open `Settings → Download Clients`
- Add **qBittorrent** (or edit an existing one)
- Enter the full url like: `http://localhost:7476/proxy/abc123def456ghi789jkl012mno345pqr678stu901vwx234yz`
- Leave username/password blank and press **Test**
- Leave basic auth blank since qui handles that

For cross-seed integration with autobrr, see the [Cross-Seed](#cross-seed) section above.

**cross-seed**
- Open cross-seed config file
- Add or edit the `torrentClients` section.
- Append the full url following the documentation
- `torrentClients: ["qbittorrent:http://localhost:7476/proxy/abc123def456ghi789jkl012mno345pqr678stu901vwx234yz"],`
- Save the config file and restart cross-seed

**Upload Assistant**
- Open the Upload Assistant config file
- Add or edit `qui_proxy_url` under the qBitTorrent client settings.
- Append the full url like `"qui_proxy_url": "http://localhost:7476/proxy/abc123def456ghi789jkl012mno345pqr678stu901vwx234yz",`
- All other auth type can remain unchanged
- Save the config file

### Supported Applications

This reverse proxy will work with any application that supports qBittorrent's Web API.

### Security Features

- **API Key Authentication** - Each client requires a unique key
- **Instance Isolation** - Keys are tied to specific qBittorrent instances
- **Usage Tracking** - Monitor which clients are accessing your instances
- **Revocation** - Disable access instantly by deleting the API key
- **No Credential Exposure** - qBittorrent passwords never leave qui

### Troubleshooting

**Connection Refused Error:**
- Ensure qui is listening on all interfaces: `QUI__HOST=0.0.0.0 ./qui serve`
- Check that the port is accessible from your external application

**Authentication Errors:**
- Verify the Client API Key is correct and hasn't been deleted
- Ensure the key is mapped to the correct qBittorrent instance

**Version String Errors:**
- This was a common issue that's now resolved with the new proxy implementation
- Try regenerating the Client API Key if you still see version parsing errors

## External Programs

The torrent context menu can launch local scripts or applications through configurable "external programs". To keep that power feature safe, define an allow list in `config.toml` so only trusted paths can be executed:

```toml
externalProgramAllowList = [
  "/usr/local/bin/sonarr",
  "/home/user/bin"  # Directories allow any executable inside them
]
```

Leave the list empty to keep the previous behaviour (any path accepted). The allow list lives exclusively in `config.toml`, which the web UI cannot edit, so you retain control over what binaries are exposed.

## Tracker Icons

Cached icons live in your data directory under `tracker-icons/` (next to `qui.db`). Icons are stored as normalised 16×16 PNGs; anything larger than 1 024×1 024 is rejected, so resize first if you are supplying files manually. qui automatically attempts to download a favicon the first time it encounters a tracker host, caching the result for future sessions. After a failed download it waits 30 minutes before retrying the same host, and the next retry is triggered automatically the next time that host appears in your tracker list. Set `trackerIconsFetchEnabled = false` in `config.toml` (or `QUI__TRACKER_ICONS_FETCH_ENABLED=false`) to disable these network fetches.

### Add icons manually

- Copy PNGs named after each tracker host (e.g. `tracker.example.com.png`) into the `tracker-icons/` directory. Files are served as-is, so trimming or resizing is up to you, but matching the built-in size (16×16) keeps them crisp and avoids extra scaling.

### Preload a bundle of icons

If you already have a library of icons (for example, exported from another installation) you can preload them via a mapping file placed alongside the directory: `tracker-icons/preload.json`, `tracker-icons/preload.js`, `tracker-icons/tracker-icons.json`, `tracker-icons/tracker-icons.js`, or `tracker-icons/tracker-icons.txt`.

- The file can be either a plain JSON object or a snippet exported as `const trackerIcons = { ... };`.
- Keys must be the real tracker hostnames (e.g. `tracker.example.org`). If you include a `www.*` host, qui automatically mirrors the icon to the bare hostname when missing.
- On startup qui decodes each data URL, normalises the image to 16×16, and writes the PNG to `<host>.png`.

  ```json
  {
    "tracker.example.org": "data:image/png;base64,AAA...",
    "www.tracker.org": "data:image/png;base64,BBB..."
  }
  ```

  ```js
  const trackerIcons = {
    "tracker.example.org": "data:image/png;base64,CCC...",
    "www.tracker.org": "data:image/png;base64,DDD..."
  };
  ```

- Example: [Audionut/add-trackers](https://github.com/Audionut/add-trackers/blob/8db05c0e822f9b3afa46ca784644c4e7e400c92b/ptp-add-filter-all-releases-anut.js#L768)

## API

### Documentation

Interactive API documentation is available at `/api/docs` using Swagger UI. You can explore all endpoints, view request/response schemas, and test API calls directly from your browser.

### API Keys

API keys allow programmatic access to qui without using session cookies. Create and manage them in Settings → API Keys.

Include your API key in the `X-API-Key` header:

```bash
curl -H "X-API-Key: YOUR_API_KEY_HERE" \
  http://localhost:7476/api/instances
```

**Security Notes:**
- API keys are shown only once when created - save them securely
- Each key can be individually revoked without affecting others
- Keys have the same permissions as the main user account

## Metrics

Prometheus metrics can be enabled to monitor your qBittorrent instances. When enabled, metrics are served on a **separate port** (default: 9074) with **no authentication required** for easier monitoring setup.

### Enable Metrics

Metrics are **disabled by default**. Enable them via configuration file or environment variable:

**Config file (`config.toml`):**
```toml
metricsEnabled = true
metricsHost = "127.0.0.1"  # Bind to localhost only (recommended for security)
metricsPort = 9074         # Standard Prometheus port range
# metricsBasicAuthUsers = "user:$2y$10$bcrypt_hash_here"  # Optional: basic auth
```

**Environment variables:**
```bash
QUI__METRICS_ENABLED=true
QUI__METRICS_HOST=0.0.0.0    # Optional: bind to all interfaces if needed
QUI__METRICS_PORT=9074       # Optional: custom port
QUI__METRICS_BASIC_AUTH_USERS="user:$2y$10$hash"  # Optional: basic auth
```

### Available Metrics
- **Torrent counts** by status (downloading, seeding, paused, error)
- **Transfer speeds** (upload/download bytes per second)
- **Instance connection status**

### Prometheus Configuration

Configure Prometheus to scrape the dedicated metrics port (no authentication required):

```yaml
scrape_configs:
  - job_name: 'qui'
    static_configs:
      - targets: ['localhost:9074']
    metrics_path: /metrics
    scrape_interval: 30s
    #basic_auth:
      #username: prometheus
      #password: yourpassword
```

All metrics are labeled with `instance_id` and `instance_name` for multi-instance monitoring.

## qBittorrent Version Compatibility

> [!NOTE]
> qui officially supports qBittorrent 4.3.9 and newer as the baseline. The features below may require newer builds as noted, and anything older than 4.3.9 might still connect, but functionality is not guaranteed.

qui automatically detects the features available on each qBittorrent instance and adjusts the interface accordingly. Certain features require newer qBittorrent versions and will be disabled when connecting to older instances:

| Feature | Minimum Version | Notes |
| --- | --- | --- |
| **Rename Torrent** | 4.1.0+ (Web API 2.0.0+) | Change the display name of torrents |
| **Tracker Editing** | 4.1.5+ (Web API 2.2.0+) | Edit, add, and remove tracker URLs |
| **File Priority Controls** | 4.1.5+ (Web API 2.2.0+) | Enable/disable files and adjust download priority levels |
| **Rename File** | 4.2.1+ (Web API 2.4.0+) | Rename individual files within torrents |
| **Rename Folder** | 4.3.3+ (Web API 2.7.0+) | Rename folders within torrents |
| **Per-Torrent Temporary Download Path** | 4.4.0+ (Web API 2.8.4+) | A custom temporary download path may be set when adding torrents |
| **Torrent Export (.torrent download)** | 4.5.0+ (Web API 2.8.11+) | Download .torrent files via `/api/v2/torrents/export`; first appeared in 4.5.0beta1 |
| **Backups (.torrent archive export)** | 4.5.0+ (Web API 2.8.11+) | qui backups rely on `/torrents/export`; the backup UI is hidden when the endpoint is unavailable |
| **Subcategories** | 4.6.0+ (Web API 2.9.0+) | Support for nested category structures (e.g., `Movies/Action`) |
| **Torrent Creation** | 5.0.0+ (Web API 2.11.2+) | Create new .torrent files via the Web API |
| **Path Autocomplete** | 5.0.0+ (Web API 2.11.2+) | Autocomplete suggestions for path inputs when adding torrents or creating .torrent files |
| **External IP Reporting (IPv4/IPv6)** | 5.1.0+ (Web API 2.11.3+) | Exposes `last_external_address_v4` / `_v6` fields |
| **Tracker Health Status** | 5.1.0+ (Web API 2.11.4+) | Automatically detects unregistered torrents and tracker issues |

> [!NOTE]
> Hybrid and v2 torrent creation requires a qBittorrent build that links against libtorrent v2. Builds compiled with libtorrent 1.x ignore the `format` parameter.

## Community

Join our friendly and welcoming community on [Discord](https://discord.autobrr.com/qui)! Connect with fellow autobrr users, get advice, and share your experiences.
Whether you're seeking help, wanting to contribute, or just looking to discuss your ideas, our community is a hub of discussion and support.
We're all here to help each other out, so don't hesitate to jump in!

## Support Development

qui is developed and maintained by volunteers. Your support helps us continue improving the project.

### License Key

Pay what you want (minimum $9.99) to unlock premium themes:
- Visit [Polar.sh](https://buy.polar.sh/polar_cl_yyXJesVM9pFVfAPIplspbfCukgVgXzXjXIc2N0I8WcL) or Settings → Themes in your qui instance
- License is lifetime

### Other methods

- **soup**
  - [GitHub Sponsors](https://github.com/s0up4200)
  - [Buy Me a Coffee](https://buymeacoffee.com/s0up4200)
- **zze0s**
  - [GitHub Sponsors](https://github.com/zze0s)

#### Cryptocurrency

To get a qui license with crypto, send the transaction link to soup or ze0s on Discord.

#### Bitcoin (BTC)
- soup: `bc1qfe093kmhvsa436v4ksz0udfcggg3vtnm2tjgem`
- zze0s: `bc1q2nvdd83hrzelqn4vyjm8tvjwmsuuxsdlg4ws7x`

#### Ethereum (ETH)
- soup: `0xD8f517c395a68FEa8d19832398d4dA7b45cbc38F`
- zze0s: `0xBF7d749574aabF17fC35b27232892d3F0ff4D423`

#### Litecoin (LTC)
- soup: `ltc1q86nx64mu2j22psj378amm58ghvy4c9dw80z88h`
- zze0s: `ltc1qza9ffjr5y43uk8nj9ndjx9hkj0ph3rhur6wudn`

#### Monero (XMR)
- soup: `8AMPTPgjmLG9armLBvRA8NMZqPWuNT4US3kQoZrxDDVSU21kpYpFr1UCWmmtcBKGsvDCFA3KTphGXExWb3aHEu67JkcjAvC`
- zze0s: `44AvbWXzFN3bnv2oj92AmEaR26PQf5Ys4W155zw3frvEJf2s4g325bk4tRBgH7umSVMhk88vkU3gw9cDvuCSHgpRPsuWVJp`

---

All methods unlock premium themes — use whichever works best for you. For other currencies or payment methods, [reach out on Discord](https://discord.autobrr.com/qui).

Thank you for your support ❤️

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

GPL-2.0-or-later
