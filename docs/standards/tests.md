# Test standards

- Go tests live beside the code as `*_test.go`. Prefer table-driven tests and existing fixtures.
- A row that shows the bug or the new behavior must fail against the code before the change. A row that expects no output can pass for the wrong reason, so the table also needs a case that does produce output.
- A change to a log level, a message text, or a constant needs no test. The reviewer reads the change in the diff.
