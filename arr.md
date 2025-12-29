# ARR-driven ID searching (Sonarr/Radarr) — research + implementation plan

This document summarizes how `cross-seed` implements ARR-driven ID searching and lays out a detailed plan to add equivalent functionality to `qui` (Go backend + React UI).

## What cross-seed implements (source-of-truth: `/Users/soup/github/cross-seed`)

### User-facing configuration
- `sonarr: string[]` and `radarr: string[]` config options (CLI flags `--sonarr`, `--radarr`).
- Each entry is a full URL that **must include an `apikey` query param** (cross-seed extracts it from the URL).
- Multiple Sonarr/Radarr instances are supported; they are searched **sequentially in the order provided** (user is advised to order “most likely to match → least likely”).

Relevant files:
- `src/configuration.ts`, `src/runtimeConfig.ts`, `src/configSchema.ts`
- `src/config.template.cjs` (docs + comments)
- `src/cmd.ts` (CLI flags)

### Startup validation
On startup, cross-seed validates ARR URLs:
1. Each URL must specify `apikey` (query param) or startup fails.
2. It pings each ARR instance with `GET <base>/api` and checks the JSON response contains a `current` field.

Relevant files:
- `src/arr.ts` (`validateUArrLs`, `checkArrIsActive`, `makeArrApiCall`)
- `src/startup.ts` (calls `validateUArrLs`)

Notes from upstream ARR code (tighten-up for qui):
- Sonarr v3 API surface includes `GET /ping` and `GET /api/v3/system/status` (more explicit than relying on legacy `/api` ping semantics). For qui, prefer testing via `GET /api/v3/system/status` (auth required) and treat `/api` as a legacy fallback if needed.

### The actual “ARR-driven ID searching” behavior
During Torznab searches, cross-seed attempts to resolve the searchee title to external IDs via Sonarr/Radarr, then uses those IDs as Torznab search parameters.

#### How it looks up IDs in ARR
1. Determine which ARR(s) to query based on inferred media type:
   - TV episode/season → Sonarr only
   - Movie → Radarr only
   - Anime/video → both (Sonarr + Radarr)
2. For each relevant ARR URL, call:
   - `GET <base>/api/v3/parse?title=<title>`
   - Header: `X-Api-Key: <apikey>` (still required even though it came from the URL), plus a `User-Agent`.
3. If the parse response includes any non-zero IDs, stop and use them.
4. If no instance yields IDs, fall back to regular string-based searching.

Important nuance:
- cross-seed treats `0` and `"0"` ID values as “not present” and discards them.
- A common failure mode is: “make sure the item is added to an Arr instance” (the parse endpoint won’t return IDs if ARR doesn’t know the item).
- Sonarr “needs season or episode” in the title in some cases; cross-seed has a workaround for `MediaType.VIDEO` by appending `S00E00` before calling Sonarr.

Notes from Sonarr contract (tighten-up for qui):
- Sonarr `GET /api/v3/parse` accepts both `title` and `path` query params (path-based parsing could be a future enhancement if title parsing is weak).
- Sonarr’s parse response may include `parsedEpisodeInfo` but omit `series` when it can’t map the parsed title to a known series. Treat “no `series`” as “no IDs,” not an error.
- Sonarr `series` includes: `tvdbId` (int), `tvMazeId` (int), `tmdbId` (int), `imdbId` (string).

Notes from Radarr contract (tighten-up for qui):
- Radarr `GET /api/v3/parse` accepts `title` (no `path` param).
- Radarr parse response includes `parsedMovieInfo` and may include `movie` when it can map the parse to a library movie.
- `parsedMovieInfo` includes `imdbId` (string) and `tmdbId` (int). Even if `movie` is missing, `parsedMovieInfo.imdbId/tmdbId` may still be populated (e.g., if IDs were present in the release title); treat those as usable when present.

Relevant files:
- `src/arr.ts` (`scanAllArrsForMedia`, `getRelevantArrIds`)

#### How it decides which ID params to include per indexer
cross-seed fetches Torznab caps and inspects `supportedParams` for `tv-search` and `movie-search`, enabling ID params only if the indexer says it supports them.

IDs supported by cross-seed:
- `imdbid`
- `tmdbid`
- `tvdbid`
- `tvmazeid`

Relevant files:
- `src/torznab.ts`:
  - `parseTorznabCaps()` parses `supportedParams` into booleans
  - `createTorznabSearchQueries()` includes IDs only if supported by that indexer’s caps
  - `IdSearchParams` includes the 4 ids above

Notes from Jackett/Prowlarr (tighten-up for qui):
- Both Jackett and Prowlarr implement hard “function not available” errors when you send an ID param the indexer doesn’t support; per-indexer caps gating is not optional if we want searches to succeed reliably.
- Prowlarr validates `imdbid` as digits-only (it strips leading `t`/`tt`); sending `tt1234567` directly may error. Our current behavior of stripping `tt` before emitting Torznab `imdbid` is correct and should be kept.

#### How it changes search query construction when IDs are available
When IDs are available:
- cross-seed will often set `q` to `undefined` (omitting it) and rely on ID parameters instead for `t=tvsearch` or `t=movie`.
- It still includes `season` and/or `ep` when appropriate.

When IDs are not available:
- it falls back to the normal query string (`q`) built from the searchee title.

Relevant files:
- `src/torznab.ts` (`createTorznabSearchQueries`)

#### Caching/invalidation behavior
cross-seed caches search work keyed by the “search string” (derived from the searchee title and tv season/episode info). It also caches the last-known IDs:
- If the cached search string is unchanged, it may rescan ARR to detect ID changes; if IDs differ, it invalidates cached candidates.
- If the search string changes, it resets cached candidates and clears cached IDs to avoid unnecessary ARR scans when skipping.

Relevant files:
- `src/torznab.ts` (`CachedSearch`, `getSearchString`, and the logic around `cachedSearch.q`/`cachedSearch.ids`)

## What we already have in qui (source-of-truth: `/Users/soup/github/autobrr/qui`)

### We already support ID parameters at the Torznab layer (partially)
- Backend request model supports:
  - `imdb_id` → Torznab `imdbid`
  - `tvdb_id` → Torznab `tvdbid`
  - `season`, `episode`, `year`, `artist`, `album`
- The Jackett service already performs capability-aware filtering and has tests showing indexer caps may include `tmdbid` and `tvmazeid`, but the API model currently does not expose `tmdb_id` / `tvmaze_id`.

Relevant files:
- `internal/services/jackett/models.go` (`TorznabSearchRequest`)
- `internal/services/jackett/service.go` (`buildSearchParams`, `getPreferredCapabilities`)
- `internal/api/handlers/jackett.go` (`/api/torznab/search`, `/api/torznab/cross-seed/search`)

### Cross-seed in qui has a clear insertion point
Our cross-seed “seeded torrent search” builds a `jackett.TorznabSearchRequest` from a torrent name and parsed release metadata, then calls `s.jackettService.Search(...)`.

Wiring note (important for scope clarity):
- The Torznab search path is centralized in `SearchTorrentMatches(...)` and is used by multiple “modes”:
  - Interactive per-torrent search (`POST /api/cross-seed/torrents/{instanceID}/{hash}/search`)
  - Completion-triggered searches (completion hook → `executeCompletionSearch` → `SearchTorrentMatches`)
  - Library search runs (`POST /api/cross-seed/search/run`), which repeatedly call `SearchTorrentMatches`
- ARR-driven ID searching should be implemented in/under `SearchTorrentMatches` so it automatically benefits all of the above.
- This does not affect the torrent-file-based apply flows (`/api/cross-seed/apply`) and does not change RSS automation (which is driven by RSS polling rather than Torznab search queries).

Relevant file:
- `internal/services/crossseed/service.go` (seeded torrent search request construction)

## Proposed behavior for qui (feature spec)

### Goal
When qui searches Torznab indexers (especially for cross-seeding), it should optionally:
1. Query Sonarr/Radarr to resolve the torrent/release title to external IDs.
2. Prefer Torznab ID search parameters (`imdbid`/`tvdbid`/`tmdbid`/`tvmazeid`) when indexers support them.
3. Fall back to normal query searches when IDs are unavailable or unsupported.

### Inputs and assumptions
- User provides 0+ Sonarr instance configs and 0+ Radarr instance configs.
- ARR instances are trusted (self-hosted LAN); we can keep error handling readable, not paranoid.
- ARR parse lookup will only work if the movie/series exists in the user’s ARR database (consistent with cross-seed).

### “Which ARR to query” rules (parity with cross-seed)
- Content type `movie` → query Radarr instances only.
- Content types `tvshow` / `tvdaily` / `anime` → query Sonarr instances only (or Sonarr first, then Radarr only if you want to support odd setups).
- Unknown/other → skip ARR lookup (or allow an “all ARR” fallback, but keep it off by default to avoid excess calls).

### “Which IDs to use” rules
- From Radarr parse results, prefer (in order):
  - `tmdbId` (best coverage for movies)
  - `imdbId`
- From Sonarr parse results, prefer (in order):
  - `tvdbId` (classic TV identifier)
  - `tvMazeId`
  - `imdbId` (sometimes present)

Then, for each indexer:
- Only send ID params that the indexer’s Torznab caps indicate it supports.
- If an indexer supports none of the available IDs, run the normal query search for that indexer.

### Search query construction rules (parity where it matters)
- If using ID search for an indexer, prefer `t=movie` or `t=tvsearch` and omit `q` (or set it empty) so IDs drive matching.
- Still include `season`/`ep` when we have them (from our existing `rls` parsing).
- Keep a fallback to the current behavior (string query) when ARR lookup fails or yields no IDs.

Compatibility note:
- Some Torznab implementations historically discouraged sending both `q` and `imdbid`; omitting `q` when doing ID searches avoids that entire class of compatibility issues.

### Caching rules (recommended)
Two caches are useful and low-risk:
1. **ARR parse result cache** (new): key by `(<contentType>, <original torrent name>)` and TTL it (e.g., 30–120 minutes).
2. **Cross-seed search cache** (already exists): can optionally store “IDs used for this search” so UI/history can show whether ARR lookup was active.

## Plan

Implement ARR-driven ID searching in qui by adding an ARR integration module (Sonarr/Radarr parse lookups), extending Torznab search requests to support the missing ID types, and wiring the ARR-derived IDs into cross-seed search flows with capability-aware per-indexer query construction and caching. This stays focused on cross-seed/search behavior and does not attempt to rework unrelated automation logic.

## Scope
- In: Sonarr/Radarr connection management, ARR parse lookups, Torznab ID param support (`imdbid`, `tvdbid`, `tmdbid`, `tvmazeid`), cross-seed seeded-torrent search integration, tests + docs.
- Out: Full ARR feature parity (command dispatch, queue management, import logic), Prowlarr integration changes, any changes to qBittorrent proxy features.

## Action items
[ ] Define the “ARR instance” data model (type, name, base URL, API key, enabled, priority order) and decide storage: new table (recommended) vs embedding in `cross_seed_settings` JSON.
[ ] Add SQL migration(s) under `internal/database/migrations/*.sql` and a store layer for ARR instances (CRUD + list in priority order), mirroring existing patterns like `TorznabIndexerStore`.
[ ] Add a persisted ARR parse cache (recommended):
[ ] - Create an `arr_id_cache` table keyed by normalized title hash + content type with `imdb_id`, `tmdb_id`, `tvdb_id`, `tvmaze_id`, `arr_instance_id`, `expires_at`.
[ ] - Implement negative caching (store “no IDs found” for a short TTL like 1h) to avoid repeated ARR lookups when items aren’t present in Sonarr/Radarr.
[ ] - Implement expiry cleanup (periodic job/ticker or opportunistic cleanup during writes).
[ ] Add a backend `internal/services/arr` (or `servarr`) package:
[ ] - Implement `Ping()` (for “Test connection”): prefer `GET /api/v3/system/status` (Sonarr) / the equivalent Radarr status endpoint; keep `/api` ping as legacy fallback if you want parity with cross-seed.
[ ] - Implement `ParseTitle(title)` calling `GET /api/v3/parse?title=...` with `X-Api-Key` header and a short timeout.
[ ] - Define typed response structs for both Sonarr and Radarr parse shapes (avoid `map[string]any`); normalize IDs into a shared `ExternalIDs` struct and treat `0`/`"0"` as empty.
[ ] Add API endpoints for ARR management (recommended under `/api/settings/arr` or `/api/integrations/arr`):
[ ] - `GET` list instances, `POST` create, `PUT/PATCH` update, `DELETE` remove, `POST /test` to validate connectivity + permissions.
[ ] - Return masked API keys (or omit) from read endpoints; only accept raw keys on create/update.
[ ] - Add a debug endpoint like `POST /.../arr/resolve` that returns IDs for `{ title, contentType }` (include “from cache” + “which ARR instance” in response).
[ ] Update OpenAPI (`internal/web/swagger/openapi.yaml`) to include the new endpoints and schemas; run `make test-openapi` after changes.
[ ] Extend `internal/services/jackett/models.go` `TorznabSearchRequest` to include `tmdb_id` and `tvmaze_id` fields (and optionally `trakt_id` later, but keep out of scope for now).
[ ] Extend `internal/services/jackett/service.go`:
[ ] - Teach `buildSearchParams` to emit `tmdbid` and `tvmazeid` when present.
[ ] - Fix/adjust `getPreferredCapabilities` so it prefers `movie-search-tmdbid` when `tmdb_id` is set (today it incorrectly keys off `tvdb_id`), and prefers `tv-search-tvmazeid` when `tvmaze_id` is set.
[ ] - Extend the Torznab search cache fingerprint/signature to include `tmdb_id` and `tvmaze_id` (see `internal/services/jackett/service.go` cache signature payload; it currently includes `IMDbID` + `TVDbID`).
[ ] Add a capability-aware “omit q when doing ID search” rule:
[ ] - Option A (closer to cross-seed): if any ID field is set and we’re in `movie`/`tvsearch`, allow `req.Query` to be empty and do not set `q` at all.
[ ] - Option B (less invasive): always set `q`, but allow per-indexer logic to clear it when the indexer supports an ID param. (Pick one and standardize.)
[ ] Ensure “ID search” never reduces indexer coverage vs today:
[ ] - Today, our Jackett capability filtering can skip indexers when parameter-specific caps (e.g. `movie-search-tmdbid`) aren’t present.
[ ] - Desired behavior (parity with cross-seed): if an indexer doesn’t support any of the available ID params, fall back to a normal `q` search for that indexer rather than skipping it.
[ ] - Implementation guidance (pick one):
[ ]   - Per-indexer param pruning: build per-indexer Torznab params; delete unsupported `*id` keys for that indexer (and ensure `q` is present when IDs are removed).
[ ]   - Dual-pass search: run an ID-only search against ID-capable indexers and a query search against the rest, then merge/dedupe results (keeping search history/outcomes consistent).
[ ] Wire ARR lookup into `internal/services/crossseed/service.go` seeded-torrent search:
[ ] - After content type detection and before building `TorznabSearchRequest`, call the ARR parse lookup (using the original torrent name) for the relevant ARR type(s).
[ ] - If IDs are returned, set `searchReq` ID fields and record “IDs used” in logs and (optionally) search history metadata.
[ ] Add caching around ARR parse lookups to prevent repeated calls when users repeatedly search the same torrent(s) from the UI.
[ ] (Optional) Add an ID-aware ranking boost when search results themselves include IDs:
[ ] - If returned `imdb_id`/`tvdb_id`/etc matches the ARR-resolved IDs, bump ranking and/or surface a “Matched by IDs” badge in the UI.
[ ] Add/extend tests:
[ ] - Unit test the ARR client parse normalization (0-values dropped, correct ID extraction).
[ ] - Unit/integration-ish test the cross-seed search request builder to ensure ARR-derived IDs are propagated into `TorznabSearchRequest`.
[ ] - Unit test Jackett param building includes `tmdbid`/`tvmazeid` and capability preference selection behaves as intended.
[ ] Add UI in `web/src/pages/CrossSeedPage.tsx` (or a dedicated Integrations page):
[ ] - Form to manage Sonarr/Radarr instances (URL, API key, enabled, priority).
[ ] - “Test connection” button per instance.
[ ] - Small indicator in seeded search UI showing whether ARR IDs were found/used for the last search (optional but helpful for debugging).
[ ] Update docs:
[ ] - Add a short section to `docs/CROSS_SEEDING.md` explaining ARR-driven ID searching, its benefits, and the key limitation (“item must exist in Sonarr/Radarr”).
[ ] - Mention supported IDs and how capability affects behavior.

## Open questions
- Where should ARR configs live: in the cross-seed settings UI (fastest) or in a shared “Integrations” area used by other features later?
- Should ID searching apply only to cross-seed searches, or also to the global Torznab Search page (with a toggle)?
- What is the preferred policy for `q` when IDs are set: always omit, omit only when supported, or keep `q` for safety?
