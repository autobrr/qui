# Frontend standards

## React Effects

- Use `useEffect` only to sync with external systems: DOM, subscriptions, network.
- Avoid derived state in Effects; calculate during render or use `useMemo` for expensive compute.
- Put user-driven logic in event handlers.
- To reset state, prefer a `key` or render-time adjustment.
- Fetch Effects must guard stale responses with cleanup/abort.
- Reference: https://react.dev/learn/you-might-not-need-an-effect

## React state

- If a component holds two or more `useState` values that change together, or that must not be true at the same time (`isLoading` and `error`), put them in one `useReducer`. Type the state and the actions as discriminated unions, so that a test can send actions to the reducer and check the next state.
- A single value that changes on its own stays in `useState`.

## Field help

- Field help goes in a tooltip on the field label. Use `FieldHelp` from `@/components/ui/field-help`. Do not add a help paragraph under the control.
- Keep this text inline, never in a tooltip: error and validation messages, warnings about data loss or actions the user cannot undo, and text the user must read before they choose.
- Per-option text in a radio group or a checkbox list stays inline. The user compares the options side by side and cannot do that through hovers.
- Status text, computed previews, section intros, and empty states are not field help. The rule does not apply to them.
- If the help text only repeats the label, delete it. Do not move it to a tooltip.
- If the first element that can take focus in a dialog is a `FieldHelp` trigger, the dialog focuses it on each open, and its tooltip stays open. Pass an `onOpenAutoFocus` handler that calls `preventDefault()` and then focuses the dialog content, as `DirScanTab.tsx` does. Without the second call, focus stays outside the dialog.

## Query cache filters

- A filter for `setQueriesData`, `invalidateQueries`, or `removeQueries` starts with the `queryKey` prefix of the feature. A bare `predicate` that reads key positions also matches the keys of other features, for example `["orphan-scan", id, "runs", 5]`. Their data has a different shape, and the update throws.

## Mutations

- `isPending` of a `useMutation` follows only the latest call. When one mutation sends two or more requests at the same time, keep a pending flag of your own that clears when all of them settle. Without it, the dialog shows as idle while a request still runs, and the user can submit again.

## Tables

- In a table whose last column holds row actions, set the `pinEnd` prop on the `TableHead` and on each `TableCell` of that column. Import both from `@/components/ui/table`. Do not write your own sticky classes. When long text makes the table wider than its container, the table scrolls sideways, and the actions stay in view. Some browsers hide the sideways scrollbar, so without `pinEnd` the user does not see the actions.
- `pinEnd` gives the column the card background color. Use it only on a table that sits on a card.
- Do not set `display: flex` on a `TableCell` with `pinEnd`. Put the flex layout on a `div` inside the cell.

## Page layout

- Each authenticated page, except the torrent list and the instance pages, renders one `Page` from `@/components/layout/Page`, also while it loads and in its empty states. `Page` owns the padding, the title, the description, the place of the controls, and the gap to the content. A page writes none of these itself, and it does not put itself in a centered `container`.
- The controls of a page, for example the instance picker or a period picker, go in `controls` of `Page`, each at the default height (`h-9`). They stay together: beside the title when they fit there, and in one row below the description when they do not, and always on a phone. The title row is as high as a control, so the controls never move the title or the description. A status that is not a control, for example a badge, goes in `badge`. On a phone the controls stay on one row, so each control must shrink: give a long label `min-w-0` and a `truncate` span, and pair a fixed width such as `!w-[240px]` with `min-w-0`. Check the header with a long instance name at 320 px.
- `Page` puts its direct children 24 px apart. A page that fills the height and scrolls inside itself, as RSS does, sets `fill`.
- A page with more than one section uses `SectionedPage` from `@/components/layout/SectionedPage`. The page translates every nav label before it passes the groups. Keep the section in the `tab` URL search parameter, and drop an unknown value to the default section, as `components/cross-seed/tabs.ts` does. A phone shows the list of sections while `tab` is empty, so the route passes `undefined` for an empty `tab`, and navigates with the `replace` value that `onSectionChange` gives, as `routes/_authenticated/settings.tsx` does.
- A tab of Settings builds its cards, lists, forms, and questions from `@/components/kit`. `src/components/kit/AGENTS.md` lists the modules. ESLint blocks the card, dialog, alert dialog, sheet, and drawer of `@/components/ui` in the Settings folders.
- In a card footer, Save is at the right. Run controls (Run now, Start run, Cancel, Dry run) are at the left. A section that runs a job builds its `RunState` in a pure function in `lib/run-state.ts` and renders `RunFooter`. Cancel always asks first.
- A switch row has the label and its help at the left and the switch at the right, as `AutoResumeSwitch` in `components/cross-seed/SourceCardFields.tsx` does.
- A top-level card uses `CardHeader` and `CardTitle` without a size or a spacing class, so that the title and the description of each section are the same distance apart. Only a small card inside a card can change its title size. A button in a card header goes in `CardAction`, as `Section` in `components/kit/Section.tsx` does. A header with `flex flex-row` wraps the title and the description in one `<div className="space-y-1.5">`, as the Scan Directories card in `components/cross-seed/DirScanTab.tsx` does.

## Touch screens

A phone has no pointer that hovers, so a phone user never sees what only a hover starts.

- A preload that a hover starts must also start on focus, or when the list that holds the item opens. The router `Link` already preloads on a touch, because `router.tsx` sets `defaultPreload: "intent"`.
- A `title` attribute does not show on a touch screen. When a cell cuts its text with an ellipsis and only its `title` holds the full text, a tap on the row must open a detail with the full text, or the cell must wrap the text. The `Tooltip` from `@/components/ui/tooltip` already opens on a tap.
