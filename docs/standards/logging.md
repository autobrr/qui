# Logging standards

DEBUG is the default log level (`internal/config/config.go`). Thus, each DEBUG line goes into the first log that a user sends with a report.

- If a line fires once for each HTTP request, timer tick, torrent, or loop pass, put it at TRACE. If its content never changes, delete it.
- Each background loop keeps one summary line at DEBUG.
- Keep these at DEBUG: retry paths, failure paths, loop summaries, and proxy mutations (rename, delete, setLocation, reannounce).
- A line that a document promises at DEBUG can fire for each torrent only for the first few torrents of a pass. Write the rest at TRACE, and put the count in the summary line. The near-miss line of the autobrr webhook check does this.
- A cap or a dedupe on a log line in a loop covers each line in that loop that writes the same message. Send them all through one helper, and give each of them a test row with duplicate input.
- If you add a DEBUG or higher line inside a loop over torrents, files, or search results on a path that autobrr, a timer, or another program calls without the user, or raise such a line to DEBUG or higher, the path needs a log budget test. If it has none, add one. The test runs the path on a small and a large library with `logbudget.Count` from `internal/testutil/logbudget`, and calls `logbudget.AssertFlat`. The number of torrents that reach the branch of the line must grow with the library. `TestWebhookCheckLogBudget` is the example.
- A check that autobrr calls on each announce gets a silence test: a check that finds nothing writes no line at DEBUG or above (`logbudget.AssertSilent`). A match or a near miss writes its summary line at DEBUG. `TestWebhookCheckWritesDebugOnlyForAMatchOrNearMiss` is the example.
- Do not log at Error inside a loop over torrents. Count the failures, and write one line after the loop. No log level can suppress an Error line, so one bad input can write thousands of lines.
- Write no line at DEBUG or above about a feature that is off or not set up (ADR 0074). A warning that the user must act on is the exception. A path that autobrr, a timer, or another program calls without the user gets a test with the feature off that wraps the path in `logbudget.AssertSilent`. `TestSearchRunLoop_GazelleOffSaysNothingAboutREDOPS` is the example. `make lint-ratchet` also counts the DEBUG and INFO lines that say a feature is off, or that are in the body of an `if` that tests for an off state, and a file cannot gain one (`docs/linting.md`, "Lines About an Off Feature"). That count reads only the syntax tree, so it does not replace the test.
