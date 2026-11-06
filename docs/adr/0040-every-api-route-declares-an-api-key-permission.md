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
- Existing keys got every Permission and no Instance limit on upgrade, so no integration broke.
