---
name: delete-comment
description: Sort the old code comments that the ratchet holds, delete the ones that say nothing, and move decisions to ADRs and excuses to issues. Run as /delete-comment [paths].
disable-model-invocation: true
---

# Delete comment

`docs/standards/simplicity.md` bans comments, and "Ratchet" in `docs/linting.md` holds the old ones. One run takes a **tranche** of old comment blocks, gives each block a **verdict**, and acts on the verdict. Keep the work files in a scratch folder outside the repo. `$W` names that folder below.

| Verdict | Action |
| --- | --- |
| `tautology`, `stale` | Delete the block. |
| `excuse` | Delete the block and open an issue for the shortcut. |
| `decision` | Delete the block and record the decision in an ADR. |
| `constraint` | Keep the block. The ratchet holds it. |

[`RUBRIC.md`](RUBRIC.md) defines the five verdicts.

## 1. Inventory

```sh
go run ./scripts/comments list > "$W/blocks.jsonl"
(cd web && node scripts/comments.mjs list) >> "$W/blocks.jsonl"
```

Each line is one block: `file`, `line`, `end`, `trailing` (code shares the line), and `text`. The markers that a tool reads are not in the list.

Pick the tranche, in whole files:

- The paths in the arguments, when the user gives them.
- With no arguments, about 4,500 blocks. Skip each file that an open pull request changes (`gh pr list --state open --json files`), and skip the high-churn areas that `AGENTS.md` names.

Write the blocks of the tranche to `$W/tranche.jsonl`. Tell the user the size of the tranche, the number of blocks that remain, and the comment-only lines that remain:

```sh
jq -s 'map(select(.trailing | not) | .end - .line + 1) | add' "$W/blocks.jsonl"
```

Done when `$W/tranche.jsonl` holds every block of each file in the tranche.

## 2. Verdicts

Split the tranche by package into batches of about 700 blocks. Give each batch to one classifier subagent, with the path of `RUBRIC.md` as its brief, the path of its batch file, and the path of its output file. A classifier only reads the repo. Without subagents, apply the rubric yourself. Join the output files into `$W/verdicts.jsonl`.

Done when `$W/verdicts.jsonl` has exactly one line for each block of the tranche, matched on `file` and `line`.

## 3. Check each verdict that costs something

A classifier sees one batch. You answer for the result, so read the code yourself for:

- **Each `excuse`.** The shortcut is real when you can name its trigger and the wrong result. Change the verdict to `constraint` when the code is right, or when the note records a ceiling that the maintainer accepted, such as a `ponytail:` note.
- **Each `decision`.** It must pass the three tests in the rubric. When an ADR or a file under `docs/` already records it, the block goes with no new ADR: write the name of that file in `ref`. When a test fails, change the verdict to `constraint`.
- **Each `stale`.** Name the code that contradicts the block.
- **30 `tautology` blocks of each batch.** When more than 2 are wrong, send the batch back to a classifier with the misses.

Done when each `excuse`, `decision`, and `stale` line carries your own reason in `why`.

## 4. Issues and ADRs

- **Excuse.** Search the open issues first. Open one issue for each shortcut that a user can notice, and one shared issue for the small cleanups of the run. Describe the wrong behavior, its trigger, and the fix. `docs/agents/issue-tracker.md` and the privacy rules in `AGENTS.md` apply.
- **Decision.** Write one ADR for each decision, also when several blocks state it. Take the next free number in `docs/adr/` and the form of the newest ADRs. State what the code and the block show, and nothing more.

Done when each `excuse` line names its issue in `ref`, and each `decision` line names its ADR file in `ref`.

## 5. Delete

```sh
jq -c -n --slurpfile v "$W/verdicts.jsonl" \
  '($v | map({key: "\(.file):\(.line)", value: {cat}}) | from_entries) as $cat
   | inputs | . + $cat["\(.file):\(.line)"] | select(.cat != "constraint")' \
  "$W/tranche.jsonl" > "$W/doomed.jsonl"
go run ./scripts/comments strip "$W/doomed.jsonl"
(cd web && node scripts/comments.mjs strip "$W/doomed.jsonl")
```

Both commands delete a block only when its text still starts on its line, and write a file only when its code is the same without the comments: the same Go tokens, or the same TypeScript tokens and emitted JavaScript. Act on each line that they print:

- `skipped`: the file changed after the inventory. List that file again and give the block a new verdict.
- `the code changed`: a JSX comment separates two texts, so the page would change, or a comment separates two words that would join. Change those verdicts to `constraint` and run the command again.

Then lower the counts:

```sh
go run ./scripts/ratchet -update-comments
(cd web && git diff --name-only --relative -- '*.ts' '*.tsx' '*.js' '*.jsx' '*.mjs' | xargs pnpm exec eslint --prune-suppressions)
```

Done when both strip commands print `skipped 0` and `go run ./scripts/ratchet` passes.

## 6. Report

Run the checks that `AGENTS.md` asks for before a push. Then report to the user:

- The number of blocks for each verdict, and the lines that the diff removes (`git diff --shortstat`).
- Each issue and each ADR, with the block that it replaces.
- The `constraint` blocks that stay, and the totals that `go run ./scripts/ratchet` prints.
- Each bug that a comment led you to and that this run did not fix.
