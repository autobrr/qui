# Threat model

## What qui does and where untrusted input enters

qui is a self-hosted web UI that manages one or more qBittorrent instances. One user runs it, often behind a reverse proxy that is open to the internet. The binary is a Go server (`cmd/qui`, `internal/`) with an embedded React frontend (`web/`).

Untrusted input enters at these points:

- HTTP requests before login: `/api/auth/*`, the OIDC routes, `/api/themes`, and `/health`.
- Webhooks for other apps: the cross-seed and dir-scan webhooks. They accept an API key in the `apikey` query parameter.
- Torrent metadata from qBittorrent and from `.torrent` files: names, file paths, comments, tracker URLs.
- Responses from trackers, Torznab indexers (Jackett, Prowlarr), Sonarr, Radarr, and notification targets.
- Requests from other apps (autobrr, Sonarr, Radarr) to the qBittorrent reverse proxy in `internal/proxy`. These requests use client API keys.

A logged-in user is trusted. Data that a torrent author or a remote server controls is not trusted, even when a logged-in user causes qui to read it.

## Components that matter most

- Authentication and session handling: `internal/auth`, `internal/api/middleware`, `internal/api/handlers/auth.go`, `internal/api/handlers/oidc.go`.
- The reverse proxy and the client API keys: `internal/proxy`, `internal/api/handlers/client_api_keys.go`.
- External programs, which run commands with torrent data as arguments: `internal/externalprograms`, `internal/services/externalprograms`.
- Outbound requests to URLs that come from torrents or from settings (SSRF): `internal/services/trackericons`, `internal/services/notifications`, `internal/services/jackett`, `internal/services/arr`.
- File operations with paths from torrents or from qBittorrent (path traversal): `internal/fsops`, `internal/services/crossseed`, `internal/services/dirscan`, `internal/services/orphanscan`, `internal/backups`.

qui runs on Linux, macOS, and Windows. A path or command issue that occurs on Windows only is in scope.

## How to exercise it

- `make build` builds the frontend and the `qui` binary in `/src`.
- `go test -count=1 ./...` runs the Go tests. The tests use local fakes only.
- `./qui serve` starts the server on port 7476.

## How we rate severity

- Critical: remote code execution, or a full authentication bypass, without a login.
- High: code execution or file writes from torrent data, SSRF that reads the response, path traversal out of the save path, or a way to get access to a qui session or API key.
- Medium: SSRF that cannot read the response, stored XSS from torrent data, and denial of service without a login.
- Low: issues that need a logged-in user to attack their own instance.

## Anything to leave alone

- A logged-in user can change qBittorrent settings, run external programs that they configured, and connect qui to any URL. These are features.
- `documentation/` is the getqui.com website and is out of scope.
- Do not report missing rate limits on routes that need a login.
