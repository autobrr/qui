---
status: accepted
date: 2026-11-05
---

# Every API route declares an API key permission

An API key holds Permissions: an area and a level (read, run, or write). Before this change, any valid key reached every route, including key management, system update and restart, and SSH credentials. Now each route, where it is registered, states the Permission that a key needs, or that it is a Browser-only route. A test walks the router and fails on a route that states neither, so a new route cannot ship open to every key. A small set of routes that hold no private data (current user, icons, theme list, version, the event-only stream) are declared open to every key.

An API key with an Instance limit reaches only routes under one instance in its limit. Cross-seed, Dir Scan and the routes that span instances refuse it with 403, instead of narrowing their results to the allowed instances. Narrowing would change every cross-instance handler, and a handler that forgets to narrow leaks the other instances. A later change can narrow these routes without breaking any key.

## Considered Options

- One read-only or full-access switch per key: a read-only key could still read the credentials of instances, arr and indexers.
- A Permission per route: about 170 rows in the dialog.
- Infer the level from the HTTP method: a POST can be a read (check duplicates, previews) or a run (apply now).

## Consequences

- `run` does not include `read`, because settings hold credentials. autobrr calls this level `invoke`.
- Exporting a .torrent file and downloading a backup need write, because those files hold passkeys.
- A key without `Torrents: write` gets no tracker passkey from any route that returns announce URLs: each URL shows only its scheme and host, and a magnet link needs `Torrents: write`. A web seed link, the download link and page link of an RSS article, and each other field of an RSS feed or article that holds a link or HTML, such as a description or a feed title, can hold a passkey too, and they show at most a scheme and host, while the text a user types into a rule, its name and its filters, shows as it is. An RSS feed URL, a feed or folder named by its URL, an article ID that holds a link, and each feed of an RSS rule show their scheme and host, if any, and the first 8 hex characters of a SHA-256 of the full value, so that two feeds on one tracker stay apart and the URL of a feed equals the entry of a rule on that feed. When such a key sends a short form back, to save a rule, to set a feed URL, or to act on a feed, folder, or article, qui turns it back into the full value, and a short form that matches nothing, or more than one feed URL, feed, folder, or article, gets a 400 error. In an indexer search result, the details link shows the same way, the download link is empty, and the GUID is a hash that a cross-seed apply accepts. A backup download also holds passkeys, and it needs `Backups: write`. A key without write and a key with write on the same live stream are in separate groups, because the subscribers of a group share one payload.
- A key without `Torrents: write` cannot test a passkey through an expression filter. The expression reads a copy with tracker origins and an empty magnet URI. The sync cache and internal callers keep the full values.
- A key without `Notifications: write` gets each notification target URL as its scheme only, because the URL holds the token of the service. A signed-in browser and a key with write get the full URL.
- Existing keys got every Permission and no Instance limit on upgrade, so no integration broke.
