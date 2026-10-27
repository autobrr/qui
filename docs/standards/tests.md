# Test standards

- Go tests live beside the code as `*_test.go`. Prefer table-driven tests and existing fixtures.
- A row that shows the bug or the new behavior must fail against the code before the change. A row that expects no output can pass for the wrong reason, so the table also needs a case that does produce output.
- If a helper returns a value that the caller writes as a durable decision, for example a cooldown stamp or a delete, test the caller expression that writes it. A test of the helper alone does not cover that expression.
- To test a qBittorrent timeout or cancellation path, put a `&Client{Client: qbt.NewClient(...)}` into `pool.clients` that points at an `httptest` server whose handler waits on `<-r.Context().Done()`. `pool_test.go` has examples. Do not use `NewClientWithTimeout`, because it logs in at once on a detached `context.Background()`. To find a cancellation, use `isContextStopped`, not `errors.Is`. The go-qbittorrent retry wrapper flattens the error, so `errors.Is` does not find `context.Canceled`.
- A change to a log level, a message text, or a constant needs no test. The reviewer reads the change in the diff.
- Do not add a test that does only one of these things:
  - It repeats a type or a constant.
  - It checks that a mock got a call, and checks nothing about the result.
  - It checks that a function returns the error from a dependency without a change.
  - It adds a table row that runs the same branch as another row. The match corpus in `internal/services/crossseed/match_corpus_test.go` is an exception, because it grows freely.
  - It checks a CSS class string or static markup in a frontend test.
- To wait for a goroutine, use a channel. To test code that waits on a timer, run the test in `synctest.Test`. In that test, `time.Sleep` moves a fake clock and takes no real time. Outside `synctest.Test`, do not use `time.Sleep`. A real sleep makes the test slow, and the test fails at random on a slow CI runner.
- A test that touches a database, also through a store or a service, does not run in `synctest.Test`. On the fake clock, the statement cache closes its statements, and the test fails at random with "statement is closed".
- If a test comes from a bug fix, put `// regression: #N` on the line above it. N is the number of the fix PR or the issue. Before you remove a test with this tag, show that another test catches the same bug.
