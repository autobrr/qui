---
status: accepted
date: 2026-11-09
---

# Every Action surface reports the picked Bulk action to one module

One module beside the selection provider owns the Bulk actions: the open dialog, the values that the dialog shows, the confirmation rule, the delete sequence with cross-seeds, the instance of each request, and Export. The context menu, the header bar, and the mobile action bar report the action that the user picked, and nothing else. Before this decision, each of the three rendered its own dialogs with its own rules. A fix reached one of them only, and the desktop layout mounted two sets of dialogs. Issue #3956.

The desktop table or the mobile cards publish one live Action target, with the list facts and the sort. The module reads it for the dialog and for the request, so an open dialog follows the selection. ADR 0034 still builds the target. Its rule that each handler takes the Action target now applies inside the module, because an Action surface holds no handler.

The module sits in the app layout, not in the table, because the header bar is outside the torrent page.

Two lint rules guard the module. `no-restricted-imports` blocks an import of a bulk action dialog outside the module. `no-restricted-properties` blocks a call to the bulk action request outside the module. The details panel and the filter sidebar are exempt, because they do not act on the selection.

## Considered options

- **One shared dialog component and hook, mounted by each surface.** Rejected: the rules exist once, but the desktop layout keeps two mounted copies with separate state.
- **The surface passes the Action target with the pick.** Rejected: the module then keeps a copy for the life of the dialog, and ADR 0034 does not allow a dialog with its own copy of the selected torrents.

## Consequences

- A new Action surface sends a pick. It imports no dialog and builds no request.
- A new Bulk action adds its dialog and its request to the module.
- The Copy items, the cross-seed tools, and External programs stay on their surface.
- #3784 and #3737 change one function, the one that picks the instance of a request.
