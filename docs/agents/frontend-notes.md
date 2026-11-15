# Frontend notes

## Torrent details

`useTorrentDetailTab` (`web/src/hooks/useTorrentDetailTab.ts`) owns when the General, Content, Trackers, Peers, and HTTP Sources tabs of `TorrentDetailsPanel.tsx` and the piece bar fetch; a new fetch rule for these goes in the hook. General merges the live stream row over the properties that load one time, and polls the row only while the stream is not usable. Content and the piece bar reload on a change of progress or state in the live row. Trackers reloads when the tracker count or the Tracker health of that row changes.

`useSyncStream` listeners receive raw frames. A delta for unchanged rows carries an empty `torrents` list and the previous `total`, so a handler clears its row only on `total === 0` and keeps the previous row on an empty list.

## getqui.com demo

`pnpm build:demo` builds the unchanged app in Vite mode `demo` for the landing page at getqui.com/demo/. `web/src/demo/` replaces `window.fetch` and `window.EventSource` with an in-memory store; there is no backend.

- A new API call on the torrent list surface needs a route in `web/src/demo/api.ts`, or the demo answers `404 {"error": "not available in the demo"}` and the feature looks broken on the site.
- Demo-only UI branches use `isDemo` from `web/src/lib/demo.ts`. Vite folds it, so production bundles carry none of them.
- Pages outside `/instances` are redirected in the demo; do not add demo handling to them.
