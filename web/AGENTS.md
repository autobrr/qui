# Web Frontend Guidelines

## Commands

```bash
pnpm dev          # Vite dev server
pnpm build        # Type-check + production build
pnpm lint         # ESLint
pnpm lint --fix   # Auto-fix
```

## Components

Use shadcn/ui components from `@web/src/components/ui/` before building custom solutions. Available components include: accordion, alert, alert-dialog, badge, button, card, checkbox, collapsible, command, context-menu, dialog, dropdown-menu, hover-card, input, label, multi-select, popover, progress, scroll-area, select, separator, sheet, slider, switch, table, tabs, textarea, tooltip.

## General Coding Rules

- NEVER use any type
- flag duplicated logic

## Theming

Use theme color classes from `@web/src/themes/` instead of hardcoded colors for backgrounds, borders, and general text.

```tsx
// Correct - uses theme variables
<div className="bg-background text-foreground border-border" />
<button className="bg-primary text-primary-foreground" />

// Incorrect - hardcoded colors for general UI
<div className="bg-gray-900 text-white border-gray-700" />
```

**Exception:** Hardcoded Tailwind colors are acceptable for status indicators and icons where the color conveys semantic meaning that should remain consistent across all themes:
- `text-yellow-500` - warnings, running/pending states
- `text-blue-500` - informational, queued states
- `text-orange-500` - rate-limited, cooldown states
- `text-green-500` - success states (though `text-primary` is often preferred)

Theme files define CSS custom properties that enable consistent styling across color schemes (autobrr, minimal, nightwalker, etc.). Reference existing components for available semantic color classes.

## Layout Patterns

### Scrollable Dialogs

Dialogs with dynamic content (forms, validation errors, expandable sections) must be scrollable to prevent overflow on smaller viewports:

```tsx
<DialogContent className="sm:max-w-[425px] max-h-[90dvh] flex flex-col">
  <DialogHeader className="flex-shrink-0">
    <DialogTitle>Title</DialogTitle>
    <DialogDescription>Description</DialogDescription>
  </DialogHeader>
  <div className="flex-1 overflow-y-auto min-h-0">
    {/* Scrollable content */}
  </div>
  <DialogFooter className="flex-shrink-0">
    {/* Optional footer buttons */}
  </DialogFooter>
</DialogContent>
```

Key classes:
- `max-h-[90dvh]` - Cap height at 90% of dynamic viewport (handles mobile browser chrome)
- `flex flex-col` - Enable flex layout for content distribution
- `flex-shrink-0` on header/footer - Keep them always visible
- `flex-1 overflow-y-auto min-h-0` on content - `min-h-0` is essential for flex children to scroll (overrides `min-height: auto` default)

### Truncating Text with Action Buttons

When you have a row with truncatable text and action buttons that must stay visible, use CSS Grid instead of flexbox:

```tsx
// Correct - grid guarantees buttons never get pushed out
<div className="grid grid-cols-[1fr_auto] items-center gap-2">
  <div className="min-w-0">
    <p className="truncate">{longTitle}</p>
  </div>
  <div className="flex gap-1">{actionButtons}</div>
</div>

// Incorrect - flexbox can fail to constrain text in nested button/trigger elements
<div className="flex items-center gap-2">
  <div className="flex-1 min-w-0">
    <p className="truncate">{longTitle}</p>
  </div>
  {actionButtons}
</div>
```

The `grid-cols-[1fr_auto]` pattern ensures:
- First column takes available space and respects `min-w-0` for truncation
- Second column sizes exactly to fit buttons
- Works reliably with nested interactive elements (buttons, Radix triggers)

## URL Handling

TanStack Router handles base paths automatically for navigation. However, when constructing URLs outside of router navigation (e.g., for browser APIs, external links, or protocol handlers), use `withBasePath()` from `@/lib/base-url`:

```tsx
import { withBasePath } from "@/lib/base-url"

// Correct - respects base path configuration (e.g., /qui/)
const url = `${window.location.origin}${withBasePath("/add")}?param=value`

// Incorrect - breaks deployments with non-root base paths
const url = `${window.location.origin}/add?param=value`
```

Use `withBasePath()` when:
- Calling browser APIs like `navigator.registerProtocolHandler()`
- Building URLs for external services or webhooks
- Constructing absolute URLs outside of TanStack Router's `<Link>` or `navigate()`

## Internationalization (i18n)

All user-visible strings must use `react-i18next`. Never hardcode English text in JSX.

**Setup in every component with user-facing copy:**
```tsx
import { useTranslation } from "react-i18next"

const { t } = useTranslation("common")
```

**Locale files:** `web/src/locales/{en,de,fr,es-419,ja,ko,pt-BR,zh-CN}/common.json`

**Rules:**
- Add keys to ALL 8 locale files when introducing new strings
- Keys are dot-namespaced by feature (e.g., `instanceBackups.toasts.settingsUpdated`)
- Keep keys alphabetically sorted within each JSON object
- Translations must be contextual and natural, not word-for-word
- Use interpolation for dynamic values: `"Applied to {{count}} instances"`
- Match the tone and style already established in each locale's section
- Button labels: short and action-oriented
- Toast messages: concise, past tense for success ("Settings saved"), present for errors ("Failed to save")

## LSP Tools

- **`mcp__language-server__hover`** - Get type info for components, hooks, props
- **`mcp__language-server__references`** - Find component usages across the app
- **`mcp__ide__getDiagnostics`** - Check TypeScript errors before running `pnpm build`

Useful for understanding TanStack Router's generated types or tracing prop flow through component hierarchies.
