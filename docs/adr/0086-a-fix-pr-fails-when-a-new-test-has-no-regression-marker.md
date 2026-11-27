---
status: superseded by 0070
date: 2026-11-24
---

# A fix PR fails when a new test has no regression marker

`docs/standards/tests.md` asks for `// regression: #N` on the line above each test that comes from a bug fix. Before you remove a test with the marker, you show that another test catches the same bug. No check held the rule. In pull requests 4208 to 4442, the last 150 that merged, reviewers flagged a bug-fix test without the marker in 22. Each finding cost a review round and a commit.

`.github/workflows/regression-marker.yml` now runs `.github/scripts/check_regression_marker.py` on each pull request whose title starts with `fix`. The script fails when a test file gains a test case and no marker:

- **A test case** is a Go `func Test…` or `func Benchmark…`, a `t.Run` or `b.Run`, or a web `it` or `test` call. `.github/scripts/count_test_growth.py` finds these in the diff, as it does for the test growth comment.
- **A table row** is an element of a Go slice, array, or map literal whose element type is an anonymous struct, such as `[]struct{ name, want string }{…}`. That is the shape of 663 test tables in qui. `scripts/testrows` counts these rows in the file before and after the change with `go/parser`, and the check counts the difference. A literal of a named type, such as `[]qbt.Torrent{…}`, is a fixture and does not count. Neither does a set such as `map[string]struct{}{…}`, whose struct has no fields.

The marker must be in the same file, on a line that the pull request adds. `make check-regression-marker TITLE="<title>" NUMBER=<n>` runs the same script before a push.

The check fails the first commits of pull requests 4330 and 4219, before the marker commits that review asked for. It also fails pull request 3771, which merged with one new row in `capabilities_test.go` and no marker. It passes the final head of 4330 and the season pack fixtures of pull request 3297. It fails the final head of 4219, which merged with three new rows in `adder_test.go` and no marker beside them.

## Consequences

- **What it catches:** a new Go test function, a subtest, a web `it` or `test` call, and a new row of a Go table of an anonymous struct, each in a pull request titled `fix`.
- **What it misses:**
  - A row of a web table, such as an element of the array that `it.each` takes. The diff alone cannot tell such a row from a fixture.
  - A row of a Go table whose type has a name, such as `[]testCase{…}`. Few tables in qui have one.
  - A change that adds one test case or row and deletes another in the same file, because the count does not grow. A renamed test is such a change, and passes with the marker that it already has.
  - A bug fix in a pull request titled `feat`, `refactor`, or `chore`. 10 of the 22 flagged pull requests had such a title. Reviewers still report these.
- **A false positive:** a fix that adds a test case that does not cover the bug, for example a test of a new helper, fails when its file has no other marker. Put the marker on the test or the row that fails without the fix. When no test in the file covers the bug, a maintainer merges past the failed check, as for the file size limit. The check has no label and no inline exception, because the marker itself is the line that the rule asks for.
- The workflow runs on `pull_request_target` and checks out the base branch. It reads the commits of the pull request as objects only, so code from the pull request never runs. A change to the script takes effect after it merges.

## Considered options

- **Require the marker on each new test, not once for each file.** Rejected. A fix often adds a row and a helper test beside it, and the check cannot tell which one fails without the fix.
- **Find web table rows by their text.** Rejected. On the fix pull requests since 2026-10-01, a text match on `{…},` and `[…],` lines flagged the fixture torrents of the season pack and orphan scan tests as rows.
- **Run the check in `make precommit`.** Rejected. A pull request has no number before it opens, so the marker can name only an issue, and a fix without an issue could not pass precommit.
- **Ask for the marker in a review rule only.** Rejected. That is the state that produced the 22 findings.
