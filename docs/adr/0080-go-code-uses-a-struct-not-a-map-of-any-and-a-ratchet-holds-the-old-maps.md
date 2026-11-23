---
status: accepted
date: 2026-11-23
---

# Go code uses a struct, not a map of any, and a ratchet holds the old maps

Go code in qui uses a struct, not a `map[string]any` or a `map[string]interface{}`. A struct names each field and its type, so the compiler finds a typo in a key, and a reader sees the shape of a JSON reply in one place. `docs/standards/simplicity.md` asked for this, but no check enforced it, and new maps kept coming in. The auth replies used maps until #4403.

The lint ratchet counts each such map type in each tracked Go file and skips `_test.go` files, under the `mapany` key of `scripts/ratchet/allowlist.json`. The 69 maps in 21 files from before the rule start as the allowlist, and rules 1 to 3 of the ratchet apply to them.

The counter is `scripts/ratchet/mapany.go`, which reads the syntax tree, not a golangci-lint linter. None of the linters that qui runs reports a map type by its key and value. A custom linter would need a plugin build of golangci-lint on each machine and in CI. The ratchet already counts comments (ADR 0070) and off-feature log lines (ADR 0074) with its own code.

The allowlist also holds the maps that decode JSON of an unknown shape on purpose. The counter reads only syntax and cannot tell such a map from a reply that should be a struct, and it has no `nolint` marker, as the `offlog` counter has none. A held map costs nothing until its file changes. New code that reads JSON of an unknown shape reads it into a `jsontext.Value`, or into a struct of the fields that it uses.

## Consequences

- A new map of any fails `make lint-ratchet` and the ratchet job in CI, with a hint to use a struct.
- The `go` lane of the ratchet bot (ADR 0020) does not take `mapany` entries. It lists the problems with `golangci-lint --enable-only`, which does not know this counter. A fix often changes the JSON of an API reply, and a check that the JSON stays the same needs a live run, which the bot does not do.
- A fix of a held map lowers its entry in the same change, as for each linter of the ratchet.
