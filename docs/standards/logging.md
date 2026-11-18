# Logging standards

DEBUG is the default log level (`internal/config/config.go`). Thus, each DEBUG line goes into the first log that a user sends with a report.

- If a line fires once for each HTTP request, timer tick, torrent, or loop pass, put it at TRACE. If its content never changes, delete it.
- Each background loop keeps one summary line at DEBUG.
- Keep these at DEBUG: retry paths, failure paths, loop summaries, and proxy mutations (rename, delete, setLocation, reannounce).
- A line that a document promises at DEBUG can fire for each torrent only for the first few torrents of a pass. Write the rest at TRACE, and put the count in the summary line. The near-miss line of the autobrr webhook check does this.
- If you add a DEBUG or higher line to a path that autobrr, a timer, or another program calls without the user, or raise a line on such a path to DEBUG, the path needs a log budget test. If it has none, add one. The test runs the path on a small and a large library with `logbudget.Count` from `internal/testutil/logbudget`, and calls `logbudget.AssertFlat`. The library must reach the branch of the new line. `TestWebhookCheckLogBudget` is the example.
- Do not log at Error inside a loop over torrents. Count the failures, and write one line after the loop. No log level can suppress an Error line, so one bad input can write thousands of lines.
