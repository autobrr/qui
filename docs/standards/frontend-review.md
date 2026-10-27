# Frontend standards

## React Effects

- Use `useEffect` only to sync with external systems: DOM, subscriptions, network.
- Avoid derived state in Effects; calculate during render or use `useMemo` for expensive compute.
- Put user-driven logic in event handlers.
- To reset state, prefer a `key` or render-time adjustment.
- Fetch Effects must guard stale responses with cleanup/abort.
- Reference: https://react.dev/learn/you-might-not-need-an-effect

## Field help

- Field help goes in a tooltip on the field label. Use `FieldHelp` from `@/components/ui/field-help`. Do not add a help paragraph under the control.
- Keep this text inline, never in a tooltip: error and validation messages, warnings about data loss or actions the user cannot undo, and text the user must read before they choose.
- Per-option text in a radio group or a checkbox list stays inline. The user compares the options side by side and cannot do that through hovers.
- Status text, computed previews, section intros, and empty states are not field help. The rule does not apply to them.
- If the help text only repeats the label, delete it. Do not move it to a tooltip.
- If the first element that can take focus in a dialog is a `FieldHelp` trigger, the dialog focuses it on each open, and its tooltip stays open. Pass an `onOpenAutoFocus` handler that calls `preventDefault()` and then focuses the dialog content, as `DirScanTab.tsx` does. Without the second call, focus stays outside the dialog.

## Query cache filters

- A filter for `setQueriesData`, `invalidateQueries`, or `removeQueries` starts with the `queryKey` prefix of the feature. A bare `predicate` that reads key positions also matches the keys of other features, for example `["orphan-scan", id, "runs", 5]`. Their data has a different shape, and the update throws.

## Tables

- In a table whose last column holds row actions, set the `pinEnd` prop on the `TableHead` and on each `TableCell` of that column. Import both from `@/components/ui/table`. Do not write your own sticky classes. When long text makes the table wider than its container, the table scrolls sideways, and the actions stay in view. Some browsers hide the sideways scrollbar, so without `pinEnd` the user does not see the actions.
- `pinEnd` gives the column the card background color. Use it only on a table that sits on a card.
- Do not set `display: flex` on a `TableCell` with `pinEnd`. Put the flex layout on a `div` inside the cell.

## Page layout

- Each authenticated page, except the torrent list and the instance pages, starts with `PageHeader` from `@/components/layout/PageHeader`. It renders the title, the description, and the page actions. Pass several actions as a fragment, so that each action wraps below the title on its own when the row has no room.
- The page root uses `p-4 lg:p-6`. A page that scrolls with the window also uses `pb-16`. Do not put a page in a centered `container`.
- A page with more than one section uses `SectionedPage` from `@/components/layout/SectionedPage`. The page translates every nav label before it passes the groups. Keep the section in the `tab` URL search parameter, and drop an unknown value to the default section, as `components/cross-seed/tabs.ts` does.
- A page that works on one instance puts the instance picker in the actions of `PageHeader`.
- In a card footer, Save is at the right. Run controls (Run now, Start run, Cancel, Dry run) are at the left. A section that runs a job builds its `RunState` in a pure function in `lib/run-state.ts` and renders `RunFooter`. Cancel always asks first.
- A switch row has the label and its help at the left and the switch at the right, as `AutoResumeSwitch` in `components/cross-seed/SourceCardFields.tsx` does.
- A top-level card uses `CardTitle` without a size class. Only a small card inside a card can change its title size.
