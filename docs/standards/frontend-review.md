# Frontend standards

## React Effects

- ESLint reports an effect in a component as `qui/no-direct-effect`, and its message names what to write instead. ADR 0073 has the reason.
- ESLint reports a second effect, or an effect that returns no cleanup, in a hook of `effectHooks` in `web/eslint.config.js` as `qui/effect-hook-shape`. ADR 0083 has the reason.
- A fetch in a hook of `effectHooks` drops a stale response.
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
- A mutation that removes a row from a cached list cancels the list queries of the feature in `onSuccess`, before it removes the row from the cache. A list request that started before the delete finished still holds the row, and without the cancel its answer brings the deleted row back.

## Client settings

- A synced value that marks how far the user has seen, for example the time the user last opened a list, only moves forward: write it only when the new value is later than the stored one. Another browser can hold a later value, and a write that moves it back shows the changes that the user saw there as new again.

## Tables

- In a table whose last column holds row actions, set the `pinEnd` prop on the `TableHead` and on each `TableCell` of that column. Import both from `@/components/ui/table`. Do not write your own sticky classes. When long text makes the table wider than its container, the table scrolls sideways, and the actions stay in view. Some browsers hide the sideways scrollbar, so without `pinEnd` the user does not see the actions.
- A table takes no `pinEnd` when it stays as wide as the card at every card width: its name column has `w-full max-w-0 whitespace-normal`, so the name wraps, and each other column fits beside it at every width where that column shows. Show a wide column only from the container breakpoint where it fits, for example `hidden @3xl:table-cell`, and measure the table just above each breakpoint with the longest value of each column. The tables of Search, Transfers, and Shares work this way.
- `pinEnd` gives the column the card background color. Use it only on a table that sits on a card.
- Do not set `display: flex` on a `TableCell` with `pinEnd`. Put the flex layout on a `div` inside the cell.

## Selection

- A table or a list where the user picks rows and then acts on all of them shows those actions in `SelectionBar` from `@/components/kit/SelectionBar`. The page passes the count text in `summary`, the actions as children, and a step that changes the selection itself, for example "Select all 240", in `extra`. The bar owns Clear selection, the Escape key that clears the selection, and the phone layout. A page adds no Escape handler of its own.
- Render the bar after the last row, inside the block that holds the rows and reaches the edges of the card, as the Search results and the Transfers list do. The bar then rests under the last row, and while more rows follow, it stays at the bottom of the screen, above the phone nav.
- The bar sticks only while no block around it clips its overflow. Give the card `overflow-clip`, not `overflow-hidden`.

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
- ESLint bans the `title` attribute (`qui/no-native-title`), because the tooltip of the browser never shows on a touch screen. Show the `Tooltip` from `@/components/ui/tooltip`, or no tooltip. When a cell cuts its text with an ellipsis, a phone user must still reach the full text: put the text in `TruncatedText`, let a tap on the row open a detail with the full text, or let the cell wrap the text. The `Tooltip` from `@/components/ui/tooltip` opens on a tap, unless the tap lands on an enabled link, button, form field, label of an enabled field, option, or menu item at the trigger or around it. A tap on a disabled control opens it, so the tooltip can tell why. A disabled native button gets no tap, so put the trigger on a wrapper around it. A bare `TooltipTrigger`, without `asChild`, opens on every tap. Set `openOnTap` on a trigger that only explains but sits in a control, for example an info button or an icon in a row that acts. Set `openOnTap="only"` when the action of that row would cover the tooltip: the click of a tap then does not bubble to the row. `TruncatedText` opens on a tap when its text is cut off, or when it gets `tooltipContent`, and the tap still reaches the row around it. Pass `openOnTap={false}` when the action of that row hides the tooltip at once, for example a menu item that closes the menu. With `wrap`, it keeps the wrapping of the text and finds text clipped by a `line-clamp-*` class.
