# Performance checks

Before you open a PR or add commits to one, do these steps for the full PR diff.

1. Review changed code and its callers for backend and frontend performance risks. Include shared helpers, dependencies, and configuration. Consider call frequency and data size when assessing allocations, nested scans, queries, concurrency, rendering, and requests. If no performance risk applies, explain why in the PR's Performance section.
2. If a change can affect performance, you must measure before and after. If the risk is unclear, measure it. Use the merge-base with the PR's target branch as the baseline. Compare it against the latest PR code under the same workload and environment. Use representative synthetic data and local stubs for external services. Repeat runs to distinguish regressions from measurement noise.
3. Use existing benchmarks, profilers, or repeatable browser measurements. Temporary measurement code is sufficient. Committing benchmark files is optional. For Go code, run `make bench-compare`, as in [Go benchmarks](#go-benchmarks). Measure other Go code without `-race`. For rendering and interaction changes, measure the browser with a production build. Before you report a bundle or lazy-loading gain, read the precache list in the built `sw.js`. The service worker downloads each file that `globPatterns` matches on the first load and after each release, also when the app imports the file lazily. `globIgnores`, `maximumFileSizeToCacheInBytes`, and `manifestTransforms` in `web/vite.config.ts` remove files from that list. The built `sw.js` is the source of truth.
4. Record the affected paths, revisions, workload size, environment, and measurement method in the PR's Performance section. Include before/after numbers, the measured differences, and your conclusion. Choose relevant metrics, such as time, memory, allocations, request counts, or bundle size. CI results qualify only when they provide this comparison. Otherwise, measure locally, even when CI covers the tests.
5. In a `make bench-compare` table, investigate only `LOOK` rows. Resolve each `CAN'T TELL` row by hand, or say in the Performance section why not. In other measurements, investigate regressions beyond measurement noise. Fix a regression or obtain explicit maintainer acceptance of the measured cost before declaring the PR ready. After further code changes, repeat the affected measurements. If measurements are blocked, report the blocker and keep this step incomplete.

## Go benchmarks

`make bench-compare` compares the working tree with the merge base of `origin/develop`. It measures for up to 15 minutes, so stop other heavy work on the machine first. Copy its whole output into the Performance section. ADR 0093 has the limits.

- **`OK`**: copy the row and go on.
- **`LOOK`**: find the cause.
- **`CAN'T TELL`**: raise the time limit, for example `go run ./scripts/benchcompare compare -base origin/develop -timeout 1h`, or measure in another way. A new benchmark and a changed workload have nothing to compare with.
- **Changed code that no benchmark covers**: measure it in another way, or say why no performance risk applies.

When you write a benchmark:

- Measure a background pass as the whole pass at 50,000 torrents.
- Build the data before `b.Loop()`. Code before the loop counts as not covered.
- Report kept memory as `b.ReportMetric(bytes, "kept-B")`.
- A new benchmark starts at the commit that merges it, with no entry in `.bench.json`.
- When you change the workload of a benchmark, delete its entry in `starts` of `.bench.json`. It then starts at the commit that merges the change.
- A stricter limit goes in `benchmarks` of `.bench.json`, and also applies to sub-benchmarks: `"internal/qbittorrent.BenchmarkHotSort": {"sec/op": {"percent": 5, "min": 0.00005, "any": 0.01}}`.

When the check `Benchmark limits and starting commits` fails, take the change back or ask a maintainer for the label `bench-approved`. Only a maintainer sets it.

An issue with the label `bench-drift` comes from the weekly `.github/workflows/bench-drift.yml`. To accept a newer starting commit that it proposes, put the commit in `starts`.
