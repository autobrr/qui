---
status: accepted
date: 2026-11-20
---

# Request code reads a published config snapshot

qui reloads `config.toml` while it runs. The reload decodes the file into the one `domain.Config` value that the process holds, and into its existing slices. A request that read that value directly could see a reload that is not complete.

`Config.Publish` stores a copy of the config, with its own slices, behind an atomic pointer. `Config.Live` returns the last published copy, and a reload does not change a copy that it returned. Startup and each reload publish when their changes are complete. Code that runs while qui serves requests reads the config through `Live`.

## Consequences

- A reload reaches a request only as a whole.
- A new slice field in the config needs a clone in `Publish`. Without it, the published copy shares the slice with the next reload.
- A config that was never published returns itself from `Live`.
