---
status: accepted
date: 2026-11-24
---

# The Windows tests run once a night, not on a pull request

A pull request gets one Windows check: `lint.yml` compiles and vets every package and its tests on a Windows runner. `windows-tests.yml` runs the whole test suite on Windows once a night on `develop`, and on a branch when a maintainer starts it.

From 2026-10-23 (#3078), the suite ran on each pull request that changed Go code. Between 2026-11-10 and 2026-11-24, that step failed 62 times, which is 62 of the 80 failed jobs of `lint.yml`. At least 20 of the 62 passed on a rerun of the same commit. Four pull requests (#3610, #4060, #4292, #4312) fixed random failures of the step, and the failures went on. By the maintainer's count, about 2% of qui users run Windows.

## Considered options

- **Keep the suite on each pull request and fix each random failure.** Rejected. That is the state that produced the four pull requests and the 62 failures.
- **Run the suite only for a pull request that changes a package with Windows-only code.** Rejected. A change in one package can break the Windows code of another.
- **Remove the Windows tests.** Rejected. 21 of the 105 packages hold Windows-only files, among them the orphan scan, which deletes files. No other job runs that code.
- **Remove the compile check too.** Rejected. GoReleaser builds the Windows binary only on a tag and on a push to `develop`, and it compiles no test file.

## Consequences

- A change that breaks qui only on Windows can merge. The nightly run shows it within a day.
- A red nightly run blocks no merge and no release. Read the newest run before a release tag.
- For a pull request that changes Windows-only code, a maintainer runs the suite on the branch: `gh workflow run windows-tests.yml --ref <branch>`.
