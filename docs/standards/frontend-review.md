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

## Tables

- In a table whose last column holds row actions, set the `pinEnd` prop on the `TableHead` and on each `TableCell` of that column. Import both from `@/components/ui/table`. Do not write your own sticky classes. When long text makes the table wider than its container, the table scrolls sideways, and the actions stay in view. Some browsers hide the sideways scrollbar, so without `pinEnd` the user does not see the actions.
- `pinEnd` gives the column the card background color. Use it only on a table that sits on a card.
- Do not set `display: flex` on a `TableCell` with `pinEnd`. Put the flex layout on a `div` inside the cell.
