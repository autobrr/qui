# Test cleanup audit for #2618

Recorded before implementation on 2026-09-07 at commit `411814e57ddb0bbe317e79b07a4a159d92cb2957`.
Source references below use that commit's line numbers.
The scope is [#2618](https://github.com/autobrr/qui/issues/2618), from the audit in [#2576](https://github.com/autobrr/qui/issues/2576).

## Claim validation

| Finding | Verdict | Evidence and retained behavior |
| --- | --- | --- |
| 2 | Confirmed | `internal/api/handlers/torrents_add_test.go:25–817` defines a copied handler, its mocks, parsing tests, and benchmark. Repository references stay inside that file. Keep `addTorrentCall`, `addTorrentFromURLsCall`, and `jackettResponse`, which the real handler mocks use. Keep all 14 `TestAddTorrentHandler_*` tests from line 826 onward. |
| 5 | Confirmed | `internal/qbittorrent/cache_test.go:17–353,393–486` constructs `ttlcache` directly in six tests and four benchmarks. The two view helpers serve only those tests. Keep `createTestTorrents`: `integration_test.go:23,1154,1171,1189` uses it with qui filtering and statistics. |
| 10 | Confirmed | `internal/qbittorrent/pool_test.go:39–154` contains two tests inside a block comment. Neither compiles or runs. Keep every executable test, including reset, backoff, probe concurrency, timeout, cancellation, ban classification, and unhealthy-client reuse. |
| 13 | Confirmed | `internal/services/arr/service_test.go:538–618` assigns struct fields and reads them back in the four named tests. It calls no service or serializer. Keep the real lookup, positive and negative cache, cancellation, hydration, and episode-map tests. |
| 18 | Confirmed | `internal/services/jackett/category_detection_test.go:12–66` supplies a dummy content type in a local conditional. Keep `TestCategoryAssignment` at line 69: its no-category case calls `detectContentType` and `getCategoriesForContentType`. |
| 22 | Confirmed | `internal/api/handlers/backups_test.go:609–651` tests its own `windowLocation` and `getBackupDownloadURL`. No other caller uses them. The browser uses `web/src/lib/api.ts:820`, called by `InstanceBackups.tsx`. Keep all `TestDownloadRun_*` tests and archive security tests. Keep `net/url`, which `newRequestWithParamsAndQuery` uses. |
| 23 | Confirmed | `internal/api/sse/manager_test.go:858–899` assigns pending state without calling `enqueueGroup` or `processGroup`. Keep `TestServeCoalescesBurstOfUpdates` in `manager_delivery_test.go:574`: it sends 50 updates through the manager and receives HTTP delivery. Keep delivery, shutdown, nil-input, and concurrency tests. |
| 29 | Confirmed | `internal/services/automations/service_test.go:1935–1957` initializes `SpaceToClear` to zero, discards its torrents, and checks zero. It calls no production behavior. Keep the adjacent needed-view, cross-seed expansion, free-space condition, and no-panic tests. |
| 35 | Confirmed | `internal/services/externalprograms/service_test.go:23–48` constructs two empty mocks and discards them. `NewService` receives nil stores. Keep the constructor calls with empty and nil configuration and their assertions. Remove the mock declarations, local variables, blank assignments, and stale comments. |

Repository-wide searches included callers, imports, scripts, and hidden workflow configuration.
No manual entry point uses the deleted helpers, all of which live in `_test.go` files.
No finding is already resolved or rejected.

## Branch inputs and retained coverage

This cleanup removes only test-local branches and inactive code. Production guards remain.

- Add-torrent input `indexer_id=42` with no indexer service takes the copied fallback. Production returns 503, as `TestAddTorrentHandler_JackettServiceUnavailable_Returns503` asserts.
- The copied parser accepts `-5` and `0` and treats `not-a-number` as no indexer. Production rejects these inputs. The retained `NegativeIndexerID`, `ZeroIndexerID`, and `InvalidIndexerID` handler tests assert 400. `InvalidInstanceID` and `NoURLsOrFiles` also remain.
- `SuccessfulIndexerDownload`, `SuccessfulMagnetWithIndexer`, `MixedURLsAndMagnets`, and `NoIndexerID_UsesDirectURL` exercise real download and direct-add paths, including nil add responses.
- `MagnetWithIndexerPartialFailure`, `MagnetRedirectPartialFailure`, `PartialFailure`, and `DirectMultiURLPartialFailure` exercise rejection, redirects, and continuation after failure through the real handler.
- Uppercase magnets, blank entries, successful redirect completion, all-download failures, file-add errors, and comma-only separation lack matching real-handler tests in this file. Their deleted tests exercise only the copy. This cleanup does not claim new coverage for those inputs.
- The copied response queue returns an error after exhausting configured responses. The retained handler-specific mock has the same guard at `torrents_add_test.go:1390–1395`.
- The category copy maps no categories to a dummy movie type and supplied categories to unknown. Production `performSearch` at `jackett/service.go:635` infers supplied categories and can fall back to detection. The retained helper test covers the no-category path. `TestDetectContentType`, `TestGetCategoriesForContentType`, and the search-category tests remain.
- The copied backup builder omits the query for absent, empty, or `zip` formats and includes it for `tar.gz` and `tar.zst`. The browser builder remains. Real `DownloadRun` tests cover default ZIP, explicit archive formats, invalid IDs, missing or unavailable backups, and unsupported formats.
- Eligible preview mode skips cumulative updates at `automations/service.go:1345,1445`. The deleted test never reaches these guards. `TestUpdateCumulativeFreeSpaceCleared_NeededView` and `TestFreeSpaceCondition_StopWhenSatisfied` retain real helper coverage, but do not establish eligible-preview coverage.
- The removed pending-state test assigns three timestamps and checks the last assignment. Real burst coverage uses `HandleMainData` through `publishInstance`, `enqueueGroup`, `processGroup`, and HTTP delivery.

## Test boundaries and checks

The retained add-torrent validation tests use nil dependencies and return before client calls.
The other handler tests use `NewTorrentsHandlerForTesting` with in-memory adders and downloaders.
Dispatch helpers in `torrents.go:122–150` return through those mocks before concrete clients.
Backup tests use temporary manifests, files, a test database, and nil clients. `DownloadRun` reads those files.

The no-category helper reaches the local release parser, with no network call.
ARR lookup fixtures and pool probes use `httptest` servers.
SSE delivery uses a local HTTP server, a fake sync provider, and a nil client pool that stops metadata lookup.
The surviving torrent fixture feeds filtering and statistics, not a configured client.

Keep request-isolation helpers, panic tests, race coverage, and the release-parser suites.
Keep production code, dependencies, API contracts, and runtime configuration unchanged.
The existing pool helper starts a health ticker. This cleanup does not change that test setup.

PR CI runs `make test-postgres` in `.github/workflows/test.yml`.
That command runs `go test -race -count=1 -v -timeout=20m ./...` through `internal/testutil/postgres/main.go:82`.
Use CI for those tests. Run `make precommit`, `make build`, and an isolated application smoke check locally.
No Docusaurus update is needed because the change removes test code without changing user behavior.
