# Bug proofs

A scenario proves a bug fix in a real browser, and you can run it again. It runs on the build before the fix (base) and on the build with the fix (head):

```sh
node scripts/prove/prove.mjs scripts/prove/scenarios/4091-search-square-brackets.mjs a0f734b8f 00c5967fa
```

The command prints one verdict:

| Verdict | Exit | Meaning |
| --- | --- | --- |
| PROVEN | 0 | The assertion fails on base and holds on head. |
| NOT PROVEN | 1 | The sides give any other mix of pass and fail. |
| COULD NOT RUN | 2 | A side did not reach its assertion. For example, the build failed, qui did not start, or the page had no such control. That side saves `error.png`. |

With one commit, the command runs only that side. Use this while you write a scenario.

## What a run does

- `build.sh` builds qui at the commit from `git archive`, offline, and keeps the binary in `~/.cache/qui-prove/bin/<sha>/`. The next run of that commit skips the build.
- Each side runs in its own network namespace that has only the loopback interface. The run cannot reach a real host, and its ports cannot clash with other rigs.
- `run.mjs` starts `stub-qbit.py` (a stub qBittorrent with no torrents) and qui with an empty config folder. It creates a user, signs in, adds the stub as an instance, and then calls the scenario.
- The screenshots go to `~/.cache/qui-prove/runs/<scenario>-<time>/<side>/`. `pairs.html` in the run folder shows base and head side by side, paired by label.

The command needs the global Playwright package and `/usr/bin/chromium`. It finds Playwright through mise. To use other paths, set `PW` and `CHROMIUM`.

## Write a scenario

A scenario is one file with one default export, an async function that gets `t`:

| Field | Use |
| --- | --- |
| `t.stub.seed({ torrents, categories, tags })` | Add made-up torrents and categories to the stub. A torrent needs only `name`. |
| `t.stub.calls(path)` | Read the write calls that qui sent to the stub, with their form fields. |
| `t.api(method, path, body)` | Call the qui API as the signed-in user. |
| `t.synced(n)` | Wait until qui synced `n` torrents. |
| `t.page`, `t.goto(path)` | Drive the Playwright page. |
| `t.shot(label)` | Save a screenshot. Take it where the bug shows, before the assertion. |
| `t.assert` | Assert with `node:assert/strict`. Use it only for the one thing that the bug breaks. |
| `t.waitFor(fn, what)` | Poll until `fn` returns a true value. A timeout is an error, not a failed assertion. |

Only a failed `t.assert` counts as the bug. For each other check, throw a plain `Error`. Then a broken setup gives COULD NOT RUN, not a false proof. Use selectors that exist on both base and head.
