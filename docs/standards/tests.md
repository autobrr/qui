# Test standards

- Go tests live beside the code as `*_test.go`. Prefer table-driven tests and existing fixtures.
- A row that shows the bug or the new behavior must fail against the code before the change. A row that expects no output can pass for the wrong reason, so the table also needs a case that does produce output.
- If a helper returns a value that the caller writes as a durable decision, for example a cooldown stamp or a delete, test the caller expression that writes it. A test of the helper alone does not cover that expression.
- To test a qBittorrent timeout or cancellation path, put a `&Client{Client: qbt.NewClient(...)}` into `pool.clients` that points at an `httptest` server whose handler waits on `<-r.Context().Done()`. `pool_test.go` has examples. Do not use `NewClientWithTimeout`, because it logs in at once on a detached `context.Background()`. To find a cancellation, use `isContextStopped`, not `errors.Is`. The go-qbittorrent retry wrapper flattens the error, so `errors.Is` does not find `context.Canceled`.
- A change to a log level, a message text, or a constant needs no test. The reviewer reads the change in the diff.
