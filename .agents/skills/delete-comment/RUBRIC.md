# Comment rubric

You classify the code comments of one batch of the qui repository: a Go backend and a React and TypeScript frontend in `web/`. Your prompt names the batch file and the output file. Only read the repo: no edits, no builds, no tests, no subagents.

## Input

Each line of the batch file is one comment block:

`{"file": repo-relative path, "line": first line, "end": last line, "trailing": true when code shares the line, "text": the comment}`

The markers that a tool reads, such as license headers, `//go:` and `//nolint` directives, `eslint-disable`, and `// regression: #N`, are not in the batch.

## Verdicts

Give each block one verdict. Read the code around the block first. The text alone cannot show a `stale`, an `excuse`, or a `decision`.

- `tautology`: the code, a name, a type, or the test body shows what the block says. This covers step narration ("Check if X"), section banners, a doc comment that restates the name or the signature ("New creates a new Debouncer"), test narration, a struct field gloss that repeats the field name, and a reason that the next lines make plain.
- `stale`: the block does not match the code. It names something that is gone or renamed, describes behavior that the code does not have, or is commented-out code. Swagger annotations in Go comments (`@Summary`, `@Param`, `@Router`) are stale: no tool reads them, and the spec is in `internal/web/swagger`.
- `excuse`: the block lets a shortcut stand. It admits a hack, a known wrong result, a swallowed error, a "for now", "temporary", "should never happen", "best effort", or "just in case" path, a TODO, a compatibility shim that nobody asked for, or work that was put off. Use it only when the code under the block is a shortcut: you can name what is fragile or wrong, and what the proper code is. A workaround for a bug in another program is a `constraint` or a `decision`. An error that the code ignores for a sound reason is a `constraint`.
- `decision`: the block records a decision that earns an ADR. Three tests must pass: the decision is hard to reverse or costly to get wrong, a reader would ask why the code does it this way, and a real alternative lost. Typical cases are a security boundary, a departure from the obvious approach, an ownership rule between modules, a wire or storage format, and an ordering or locking rule that protects data.
- `constraint`: a true fact that the code cannot show and that a reader would get wrong without it, but that is too small for an ADR: a quirk of another program or a platform, a unit, the reason for one line, a hidden link to another file. To choose between `tautology` and `constraint`, ask: does a maintainer who reads this code, its names, and its tests learn something they need from the block? If not, it is a `tautology`.

When a block mixes a restatement with a real reason, the real reason decides.

The file names in `docs/adr/` state the accepted decisions. Read the ADRs near your area. When a `decision` or a `constraint` only repeats one of them, keep the verdict and write the file name of the ADR in `ref`.

## Output

Go through the batch one file at a time: read the file, give each of its blocks a verdict, append the lines to the output file, and move on. Expect most blocks to be a `tautology`, and `excuse` and `decision` to be rare.

`{"file": ..., "line": ..., "cat": "tautology|stale|excuse|decision|constraint", "why": "at most 15 words"}`

`file` and `line` equal the input values. Extra keys:

- `stale`: `why` says what does not match.
- `excuse`: `hack` says what is fragile or wrong, with the trigger and the wrong result for the user when you can name them. `fix` names the proper code in one sentence.
- `decision`: `decision` gives the decision and its reason in one or two sentences, checked against the code. `ref` names the ADR that already records it, when one does.

Done when the output file has exactly one line for each block of the batch. Compare the `file` and `line` keys, and fill each gap.

## Final message

Reply with the count for each verdict, each `excuse` (file, line, `hack`, `fix`), each `decision` (file, line, `decision`, `ref`), and each thing that surprised you, such as a block that reveals a bug.
