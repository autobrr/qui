---
status: accepted
date: 2026-11-25
---

# A Go benchmark difference counts by its absolute size, and drift starts at a fixed commit

`make bench-compare` labels each benchmark row of a change, and the author investigates only `LOOK`. A row is `LOOK` when the difference is real and reaches a limit:

| | Percent and size together | Any percent |
|---|---|---|
| Time | 10% or more, and 100 µs or more | 50 ms or more |
| Memory, allocated or kept | 10% or more, and 1 MiB or more | 20 MiB or more |

A row with no real difference is `OK`. A row that the runs cannot decide is `CAN'T TELL`, never `OK`. The author resolves it by hand or says why not, because some rows have no answer on the author's machine, such as a benchmark that is new on the branch.

A weekly job compares each benchmark on `develop` with a fixed starting commit, with the same limits. It measures both versions on one machine. The repository stores commits, not times.

A percent alone reports 0.6 µs of a 5 µs function and hides 100 ms of a 5 s pass. A user notices only the second. A check per change also misses a loss that grows in small steps, each below the limit.

- **What is real.** The command uses the statistics of benchstat, from `golang.org/x/perf`: the Mann-Whitney U-test on at least 10 runs of each side, in turns.
- **Long operations.** For an operation where 10% reaches the any-percent limit (500 ms, 200 MiB), noise can hide a loss that a user notices. The command adds runs until it can see 50 ms or 1%, whichever is larger. When the time limit comes first, the row is `CAN'T TELL`.
- **What a limit applies to.** The units in `limits` of `.bench.json`: time, CPU time, allocated memory, and kept memory per operation. A benchmark reports kept memory as `kept-B`. Other units, such as a count or a time per item, get no label.
- **What is compared.** The command freezes the working tree as a commit and builds both commits in worktrees, so an edit during the run changes nothing.
- **Which benchmarks run.** The command runs each benchmark of the affected packages with coverage, two times and three times. A benchmark touches the code that the third run executes again, so a change in its setup does not select it. Two runs, not one: Go runs a benchmark that counts to `b.N` once more before it measures, and two and three runs give that setup the same count. The report lists changed code that no benchmark runs that way. A changed constant, variable, type, or import without a name, and a deleted method, count as a change of the whole package, and select each benchmark that depends on that package: a constant has no coverage. A new version of Go touches every benchmark.
- **Starting commits.** A benchmark with no entry in `starts` starts at the commit that added its current workload. The workload is the benchmark function and the test code that it uses, also from the packages in `workload_dirs`. A change of `workload_dirs` needs approval, because it can move a starting commit. The 33 benchmarks that exist now start at the commit before this decision.
- **Approval.** The check `Benchmark limits and starting commits` fails when a pull request moves a starting commit, makes a limit looser, changes a workload, or removes a benchmark. It passes when a maintainer sets the label `bench-approved`. The workflow runs the code of the default branch and reads the pull request only as text, as in ADR 0079. A stricter limit and a new benchmark need no approval.
- **The weekly report.** A `LOOK` opens an issue with the label `bench-drift`, or adds a comment to the open one. It gives the total loss and the pull requests since the starting commit that touch code the benchmark uses. A benchmark that got faster gets a proposal for a newer starting commit in the same issue, with all its rows, unless one of them is `LOOK` or `CAN'T TELL`.

## Consequences

- A loss that stays below the limits in total is never reported.
- A slow path needs a benchmark of the whole pass. 1 µs per torrent is below the limit, and at 50,000 torrents it is 50 ms.
- A change of a benchmark's workload ends its history. Its starting commit becomes the commit that merges the change, so a loss before that commit no longer counts.
- A starting commit must still build. When it no longer does, the weekly job fails and names the benchmark.
- A run may take only its share of the time limit: the limit divided by the runs of both sides. A benchmark that needs longer is `CAN'T TELL` at once, so one that hangs does not use up the time of the others. The weekly job fails for it, and for a benchmark that fails in more than half of its runs on `develop` but not at its starting commit.
- When the time budget of the weekly job ends, the summary lists the benchmarks without a result, and the next run starts with them.
- A pull request cannot change its own approval check. A change to it takes effect after it merges.

## Considered options

- **A percent limit alone.** Rejected. It reports 0.6 µs and hides 100 ms.
- **Stored times.** Rejected. They depend on the machine, so a new runner looks like a regression.
- **The last week as the starting point.** Rejected. A loss that grows by 8% per week is never 10%.
- **A starting commit that follows each gain.** Rejected. One lucky run would move it, and nobody would decide it.
- **The benchstat command.** Rejected. Its output gives a percent, and the label needs the absolute difference and the confidence intervals. The package holds the same statistics.
- **The coverage of one run.** Rejected. It also counts the setup, so a change of a shared test helper would select each benchmark.
