---
status: accepted
date: 2026-11-24
---

# A test reads every route with a read-only API key and fails on a planted secret

ADR 0040 keeps credentials and tracker passkeys from an API key that holds only read Permissions. Each handler that returns an announce URL hides the passkey itself, and `TestReadKeyGetsNoTrackerPasskey` lists by hand the 9 routes that do so. A new route that returns a URL or a setting gets no check until someone adds it to that list. Pull request 3851 added the hiding, and pull request 3856 then closed a filter that still let such a key test a passkey.

`TestReadKeyGetsNoPlantedSecret` in `internal/api` covers each route without a list. The rig in `server_secrets_rig_test.go` plants a known value in each place where a secret enters qui: each stored credential (instance, SSH, Arr app, indexer, RED/OPS, TVDB, and the config file), a Client Proxy API key, a second API key, a notification target URL, the announce URLs and the proxy and email passwords that the qBittorrent stub returns, and the links of the search result that the indexer stub returns. The test then walks the router. It sends each route with a key that holds every read Permission and no other, and it fails when a response holds a planted value. It sends the same routes as a signed-in browser, which may get the passkeys and the notification target URL, and nothing else.

A route that the key reaches must answer with a 2xx status. A route that cannot do so in the rig has a row in `walkCannotAnswer` with the reason. So a new route either runs in the walk or shows in review as a new row.

With `CanReadTrackerPasskeys` changed to allow every key, the test fails on 14 routes, the torrent stream and the three search routes included. With the mask of the cross-seed keys removed, it fails on `GET /api/cross-seed/settings` and `GET /api/cross-seed/status`.

## Consequences

- **A new route** needs no change when its path parameters work with the values in `walkPathValues`. When it needs a query or a body, add a row to `walkRequests`.
- **A new place for a secret** needs one `rig.plant` call: a credential column, a config key, or a field of qBittorrent, an indexer, or an Arr app that can carry a key. The test catches only planted values.
- **What it misses:**
  - A route that needs a run or a write Permission. The walk sends no request that changes state.
  - The 13 routes in `walkCannotAnswer`, for example the download of a log file and the content file routes.
  - A secret in another form, for example its hash or its base64 text.
  - A route that answers, when the request of the walk does not reach the code that returns the secret.
- The test adds about 1.5 seconds to the tests of `internal/api`.

## Considered options

- **A secret type that masks itself when qui writes it as JSON.** Rejected as the check. The stored credentials are already safe in this way: a model holds only the encrypted text, with `json:"-"`. The passkeys came through text that qui passes on, such as an announce URL in a go-qbittorrent struct. Who may read that text depends on the Permission of the request, which a JSON method cannot see.
- **Cut the path and the query from each URL in a response to a read key.** Rejected. It also cuts links that hold no secret, such as the URL of an instance, and a secret outside a URL still passes.
- **Add each new route to the list in `TestReadKeyGetsNoTrackerPasskey`.** Rejected. That list covers a route only after someone thinks of it, which is the gap that this test closes.
