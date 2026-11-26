---
status: accepted
date: 2026-11-20
---

# Every add builds its link tree through one module

Three adds build a Link tree: the single hardlink and reflink add, the season pack add, and the Dir Scan add. Each one picked a Link base directory, applied the Directory preset, built a plan, created the tree, and rolled it back. A fix landed in one add and reached the others late: the retry after a cross-device error landed in #2983 and reached the season pack add in #3197. The link tree module now does these steps for every add in two calls: a plan that writes nothing, and a create that returns the tree or a failure with a kind. The caller keeps the regular fallback, the placement, and the facts for the Add verdict (ADR 0022). Issue #3833.

## Considered options

- **Three bodies with shared helpers, the status quo.** Rejected: each body knew the order of the steps, so a retry fix or a cleanup fix landed three times.
- **One module for the single add and Dir Scan only.** Rejected: the season pack add is the add that got the fixes late.
- **One call that plans and creates.** Rejected: the Manual match preview and the manual season pack preview show the destination before the add. The season pack add also asks the Add verdict between the plan and the create. With one call, each preview keeps its own path code.
- **The module also decides the regular fallback.** Rejected: each add has its own rule. The season pack add and a routed add (ADR 0035) never fall back. Dir Scan falls back into the scanned folder. The single add has the create-error split of ADR 0006.

## Consequences

- A link fix is one change in the module and one row in its table test.
- A season pack follows the folder rule of every add: the folder in the Preset directory must be new, and a taken name gives `<name>--<first 8 characters of the info hash>`. Two packs with one name do not share a folder.
- This changes ADR 0006 in two places. The tree create is not a field of the `linkMode` value: the module creates the tree from the link kind. The create-error split stays in `linkMode`, with two exceptions for every add: a lost connection and a failure that can leave a partial tree never fall back to regular mode.
- The `forbidigo` rule with the message `build and remove link trees through the link tree module in internal/services/linktree` blocks a direct call to the tree create and the tree remove of the filesystem backend. It sees only a call through the backend interface. The package of the link tree module and `internal/services/transfer/local.go` have exclusions. In that file, Transfer hardlinks one temporary file at a time with `HardlinkTree`. The filesystem backend has no other call that links one file. The rest of the Transfer package has no exclusion.
- The link tree module builds a Link tree under a Link base directory. A link of one file to another place is not a Link tree. Callers that link one file do not use the module. The partial pool and Transfer are examples.
- A change that lets a caller pass its own create function, or that moves the choice of the Link base directory back into a caller, reverses this decision and needs a new ADR.
