---
status: accepted
date: 2026-11-23
---

# A saved feature map lists each route and dialog with the steps to use it

`web/feature-map.json` is the feature map of the frontend. It lists each route, and each dialog that a route loads. Each entry has `steps`: how a user gets there and what the user does, one action in each string. `web/scripts/feature-map.mjs` builds the list from the TypeScript program, and a person or an agent writes the steps. `pnpm check:feature-map` runs in `make precommit` and in the Lint workflow. It fails in three cases:

1. The saved list does not match the code.
2. A route has no steps.
3. More dialogs have no steps than on the base branch.

`AGENTS.md` tells an agent to run each change live. Before this decision, an agent that changed a component had to read the code to find the page that shows the component, and the clicks that lead to it. The repo had no list of its pages and their controls. On 2026-11-23 the script found 18 routes and 83 dialogs in 55 files. The 18 routes got their steps in the same change, and each step was followed on a local qui. The 83 dialogs start without steps, and their number only shrinks, as the totals in ADR 0077.

The script reads the id, the address, and the search parameters of a route from the type of its `Route` export, which TanStack Router makes. The tabs and the deep links of a page are search parameters, so the map lists them with no reading of the route files by hand. A dialog is a component at the top level of a file that renders `AlertDialogContent`, `DialogContent`, `DrawerContent`, or `SheetContent`. The script follows the imports from each route, so each dialog names the routes that load it.

## Consequences

- A pull request that adds a route writes its steps. A pull request that adds a dialog writes its steps, or the steps of another dialog that has none.
- `node web/scripts/feature-map.mjs --where <file>` prints the routes that load a file, with their steps, and the dialogs in the file. An agent that changed the file starts there.
- The id of a dialog is its file and its name. A rename or a move gives it a new id, and `pnpm feature-map` drops the steps of the old id. The author copies them to the new id.
- Nothing checks that a step is still true after the page changes. The author follows the steps on a running qui.
- A dialog that opens through a shared component, such as `ConfirmDialog`, is in the map one time, at the shared component. Its uses are not in the map.
- A file that a route loads only with `import()` of a computed path is not in the map.
- The script finds a dialog by the name of the tag. A file that imports one of the four components under another name has no entry. No file does this today.
- The endpoints are not in the map. `internal/web/swagger/openapi.yaml` lists each one with a summary, and `TestAllEndpointsDocumented` fails when one is missing.

## Considered options

- **A scheduled run that writes the map each day.** Rejected. Between two runs the map is stale, and an agent cannot see that. The check fails in the pull request that makes the map stale, and the author of the change knows the steps.
- **Read the route files with patterns.** Rejected. The router already has the address and the search parameters of each route in its types, with the parent routes applied. A pattern would be a second reading of the same code.
- **Steps for the 83 dialogs in the same change.** Rejected. Each step needs a run on a live qui, and many dialogs need data that a stub does not have. The count holds them until a change touches them.
- **A copy of the endpoints in the map.** Rejected. It would be a second list beside `openapi.yaml`.
