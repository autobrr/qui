# AGENTS.md

The kit holds the parts that every tab of Settings builds on. ESLint blocks `@/components/ui/{card,dialog,alert-dialog,sheet,drawer}`, `vaul`, and `CommandDialog` in the Settings folders (`settingsFiles` in `web/eslint.config.js`), so a tab gets its card, form, and question from here.

Pick the module by the job:

- A card with a title, a description, header buttons, and the loading and error state of its queries: `Section`.
- An on/off setting: `SwitchRow`. A labeled control with help and an error: `Field`.
- Read-only values with labels, with copy buttons: `Facts`.
- The items of one kind, with a switch, a state, and steps per row: `ResourceList`. Its empty tile alone: `EmptyTile`. The ids of the rows that a mutation still runs for: `usePendingIds`.
- A form to add or edit an item, also an item to read or one that saves each change at once: `Editor`.
- A question before a step: `ConfirmDialog`.
- The actions for the rows that the user picked, at the end of a table or a list: `SelectionBar`. Other pages use it too. Its placement is in "Selection" in `docs/standards/frontend-review.md`.
- A page with sections, for example Settings: `SectionedPage` in `@/components/layout`.

The JSDoc on each prop is the contract. Read it before you call the module.

When a module cannot do what a tab needs, extend the module, and add a test of the new prop beside the module, as `ResourceList.test.tsx` does. A local copy beside the tab splits the look of Settings. A file that must keep its own dialog or sheet, for example because the torrent list opens it too, goes in `settingsKitAllowlist` in `web/eslint.config.js` with its reason.

Every change here reaches every tab. After a change, open each tab that uses the module at 390 px and at 1280 px.
