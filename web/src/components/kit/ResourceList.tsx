/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChevronDown, ChevronRight, Loader2, MoreHorizontal, Pencil, type LucideIcon } from "lucide-react"
import { Fragment, isValidElement, useId, useState, type ReactElement, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

import { ConfirmDialog } from "@/components/kit/ConfirmDialog"
import { EmptyTile, type EmptyTileProps } from "@/components/kit/EmptyTile"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TruncatedText } from "@/components/ui/truncated-text"
import { cn } from "@/lib/utils"

/** ok: works. warn: works, with a problem. error: does not work. off: not in use. */
export type ResourceTone = "ok" | "warn" | "error" | "off"

export interface ResourceAction {
  label: string
  icon: LucideIcon
  /** A promise keeps the confirm dialog open with a spinner until it settles. */
  onSelect: () => void | Promise<unknown>
  destructive?: boolean
  disabled?: boolean
  /** The step runs, for example a test: the icon turns into a spinner, and the action takes no second press. */
  pending?: boolean
  /** Asks first. Give it to each step that the user cannot undo. */
  confirm?: { title: string; description: ReactNode; confirmLabel: string }
}

export interface ResourceRow {
  title: string
  /**
   * Short muted facts under the title, for example the address and a count, joined with dots. A wide screen shows them
   * on one line and cuts the end. A phone wraps them onto more lines, and a long fact wraps inside itself.
   */
  meta?: readonly ReactNode[]
  /** A lucide icon, or an element such as a tracker icon, in the tile at the start of the row. */
  icon?: LucideIcon | ReactElement
  /** A colored dot and a word before the meta line, for the state of the item. */
  status?: { tone: ResourceTone; label: string }
  /** An on/off switch for the item. An item that is off shows dimmed. */
  enabled?: { checked: boolean; onCheckedChange: (checked: boolean) => void; disabled?: boolean }
  /**
   * A box at the start of the row that adds the item to a selection, for steps on several items. A checked row shows
   * tinted. In a list where some rows have a box, a row without one keeps the space, so the rows stay in line.
   */
  select?: { checked: boolean; onCheckedChange: (checked: boolean) => void; disabled?: boolean }
  /** Opens the item, usually in its Editor. The row and an Edit action both call it. */
  onOpen?: () => void
  /**
   * The steps for the item. A wide screen shows the first three as buttons and the rest in a menu. A phone shows a lone
   * step as a button, and more in a menu. The menu puts the destructive steps last, under a line. On a phone, a row
   * that only opens shows an arrow.
   */
  actions?: readonly ResourceAction[]
  /** More content under the row, for example the last errors of the item. */
  detail?: ReactNode
  /**
   * Details that a press on the row opens under it, and a second press closes. The row then shows an arrow that turns.
   * With onOpen too, the item opens only from its Edit action.
   */
  expand?: ReactNode
}

interface ResourceListProps<T> {
  /** The accessible name of the list. */
  label: string
  items: readonly T[]
  itemKey: (item: T) => string | number
  row: (item: T) => ResourceRow
  /** Puts the items under headings, in the order of the first item of each heading. Headings arrive translated. */
  group?: (item: T) => string
  /** The tile that shows when the list is empty. With onAdd, the tile is a button. */
  empty: EmptyTileProps
}

const INLINE_ACTIONS = 3

const TONE_DOT: Record<ResourceTone, string> = {
  ok: "bg-emerald-500",
  warn: "bg-amber-500",
  error: "bg-destructive",
  off: "bg-muted-foreground/50",
}

/** The items of one kind in rows of 56 px, each with its state, its switch, and its steps. */
export function ResourceList<T>({ label, items, itemKey, row, group, empty }: ResourceListProps<T>) {
  const [confirming, setConfirming] = useState<ResourceAction | null>(null)
  // The dialog keeps its text while it fades out.
  const [shownConfirm, setShownConfirm] = useState<ResourceAction | null>(null)

  const run = (action: ResourceAction) => {
    if (action.confirm) {
      setConfirming(action)
      setShownConfirm(action)
    } else {
      void action.onSelect()
    }
  }

  if (items.length === 0) return <EmptyTile {...empty} />

  const rows = items.map((item) => ({ key: itemKey(item), row: row(item), heading: group?.(item) ?? "" }))
  const selectable = rows.some((entry) => entry.row.select)
  const list = (name: string, entries: typeof rows) => (
    <ul aria-label={name} className="flex flex-col gap-1.5">
      {entries.map((entry) => <ResourceListRow key={entry.key} row={entry.row} selectable={selectable} onRun={run} />)}
    </ul>
  )

  return (
    <>
      {group ? (
        <div role="group" aria-label={label} className="space-y-4">
          {[...new Set(rows.map((entry) => entry.heading))].map((heading) => (
            <div key={heading} className="space-y-2">
              <h3 className="text-xs font-medium text-muted-foreground">{heading}</h3>
              {list(heading, rows.filter((entry) => entry.heading === heading))}
            </div>
          ))}
        </div>
      ) : list(label, rows)}
      {shownConfirm?.confirm && (
        <ConfirmDialog
          open={confirming !== null}
          onOpenChange={(open) => !open && setConfirming(null)}
          title={shownConfirm.confirm.title}
          description={shownConfirm.confirm.description}
          confirmLabel={shownConfirm.confirm.confirmLabel}
          onConfirm={shownConfirm.onSelect}
        />
      )}
    </>
  )
}

function ResourceListRow({ row, selectable, onRun }: { row: ResourceRow; selectable: boolean; onRun: (action: ResourceAction) => void }) {
  const { t } = useTranslation("common")
  const titleId = useId()
  const expandId = useId()
  const [expanded, setExpanded] = useState(false)
  const off = row.enabled?.checked === false
  const open: ResourceAction | null = row.onOpen
    ? { label: t("kit.edit"), icon: Pencil, onSelect: row.onOpen }
    : null
  const actions: ResourceAction[] = [...(open ? [open] : []), ...(row.actions ?? [])]
  // A wide screen shows the first actions as buttons, in the order of the caller. The menu puts the destructive ones last.
  const menu = [...actions.filter((action) => !action.destructive), ...actions.filter((action) => action.destructive)]
    .map((action) => ({ action, inline: actions.indexOf(action) < INLINE_ACTIONS }))
  const firstDestructive = menu.findIndex((entry) => entry.action.destructive)
  const wideMenuSplits = menu.some((entry) => !entry.inline && !entry.action.destructive) && menu.some((entry) => !entry.inline && entry.action.destructive)
  // A phone shows a lone action as a button, and an arrow when that action opens the item.
  const phoneButton = actions.length === 1 ? actions[0] : null
  const facts: ReactNode[] = [
    ...(row.status ? [<StatusLabel key="status" {...row.status} />] : []),
    ...(row.meta ?? []),
  ]

  const body = (
    <>
      <RowIcon icon={row.icon} off={off} />
      <span className="min-w-0 flex-1">
        <TruncatedText id={titleId} wrap openOnTap={false} className={cn("block text-sm font-medium [overflow-wrap:anywhere] pointer-fine:truncate", off && "text-muted-foreground")}>
          {row.title}
        </TruncatedText>
        {facts.length > 0 && (
          <span className="flex min-w-0 flex-wrap items-baseline gap-x-1.5 text-xs text-muted-foreground sm:flex-nowrap">
            {facts.map((fact, index) => (
              // The dot stays with the fact before it, so a line that wraps never starts with a dot.
              <span key={index} className="flex min-w-0 max-w-full items-baseline gap-1.5">
                <span className="min-w-0 [overflow-wrap:anywhere] sm:truncate">{fact}</span>
                {index < facts.length - 1 && <span aria-hidden="true" className="shrink-0">·</span>}
              </span>
            ))}
          </span>
        )}
      </span>
      {row.expand && <ChevronDown aria-hidden="true" className={cn("size-4 shrink-0 text-muted-foreground transition-transform", expanded && "rotate-180")} />}
    </>
  )
  const bodyClass = "flex min-w-0 flex-1 items-center gap-3 self-stretch rounded-lg px-3 py-2 text-left"

  return (
    <li aria-labelledby={titleId} className="flex items-center gap-2">
      {selectable && (
        <span className="grid w-6 shrink-0 place-items-center">
          {row.select && (
            <Checkbox
              aria-label={row.title}
              checked={row.select.checked}
              disabled={row.select.disabled}
              onCheckedChange={(checked) => row.select?.onCheckedChange(checked === true)}
              className="relative pointer-coarse:after:absolute pointer-coarse:after:-inset-3"
            />
          )}
        </span>
      )}
      <div className={cn("min-w-0 flex-1 rounded-lg border transition-colors", row.select?.checked && "bg-primary/5")}>
        <div className="flex min-h-14 items-center gap-1 pr-1.5">
          {row.expand ? (
            <button
              type="button"
              aria-expanded={expanded}
              aria-controls={expanded ? expandId : undefined}
              onClick={() => setExpanded(!expanded)}
              className={cn(bodyClass, "outline-none transition-colors hover:bg-accent/50 focus-visible:ring-[3px] focus-visible:ring-ring/50")}
            >
              {body}
            </button>
          ) : row.onOpen ? (
            <button
              type="button"
              onClick={row.onOpen}
              className={cn(bodyClass, "outline-none transition-colors hover:bg-accent/50 focus-visible:ring-[3px] focus-visible:ring-ring/50")}
            >
              {body}
            </button>
          ) : (
            <div className={bodyClass}>{body}</div>
          )}
          {row.enabled && (
            <Switch
              aria-label={t("kit.enabled")}
              aria-describedby={titleId}
              checked={row.enabled.checked}
              disabled={row.enabled.disabled}
              onCheckedChange={row.enabled.onCheckedChange}
              className="relative mx-1.5 pointer-coarse:after:absolute pointer-coarse:after:-inset-3"
            />
          )}
          {actions.length > 0 && (
            <div className="flex items-center max-sm:hidden">
              {actions.slice(0, INLINE_ACTIONS).map((action) => (
                <Tooltip key={action.label}>
                  <TooltipTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={action.label}
                      aria-describedby={titleId}
                      disabled={action.disabled || action.pending}
                      onClick={() => onRun(action)}
                      className={cn("size-8 text-muted-foreground hover:text-foreground", action.destructive && "hover:text-destructive")}
                    >
                      <ActionIcon action={action} />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>{action.label}</TooltipContent>
                </Tooltip>
              ))}
            </div>
          )}
          {phoneButton && (
            <Button
              variant="ghost"
              size="icon"
              aria-label={phoneButton.label}
              aria-describedby={titleId}
              disabled={phoneButton.disabled || phoneButton.pending}
              onClick={() => onRun(phoneButton)}
              className={cn("size-11 text-muted-foreground sm:hidden", phoneButton.destructive && "text-destructive")}
            >
              {phoneButton === open ? <ChevronRight /> : <ActionIcon action={phoneButton} />}
            </Button>
          )}
          {actions.length > 1 && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={t("kit.moreActions")}
                  aria-describedby={titleId}
                  className={cn("size-8 text-muted-foreground max-sm:size-11", actions.length <= INLINE_ACTIONS && "sm:hidden")}
                >
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="min-w-44">
                {menu.map(({ action, inline }, index) => (
                  <Fragment key={action.label}>
                    {index === firstDestructive && index > 0 && <DropdownMenuSeparator className={cn(!wideMenuSplits && "sm:hidden")} />}
                    <DropdownMenuItem
                      variant={action.destructive ? "destructive" : "default"}
                      disabled={action.disabled || action.pending}
                      onSelect={() => onRun(action)}
                      // A wide screen shows the first actions as buttons, so its menu lists only the rest.
                      className={cn("pointer-coarse:py-2.5", inline && "sm:hidden")}
                    >
                      <ActionIcon action={action} />
                      {action.label}
                    </DropdownMenuItem>
                  </Fragment>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </div>
        {row.expand && expanded && <div id={expandId} className="border-t px-3 py-3">{row.expand}</div>}
        {row.detail && <div className="border-t px-3 py-2.5">{row.detail}</div>}
      </div>
    </li>
  )
}

function ActionIcon({ action }: { action: ResourceAction }) {
  return action.pending ? <Loader2 className="animate-spin" /> : <action.icon />
}

function StatusLabel({ tone, label }: { tone: ResourceTone; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span aria-hidden="true" className={cn("size-1.5 shrink-0 rounded-full", TONE_DOT[tone])} />
      {label}
    </span>
  )
}

function RowIcon({ icon, off }: { icon: ResourceRow["icon"]; off: boolean }) {
  if (!icon) return null
  const Icon = isValidElement(icon) ? null : icon
  return (
    <span aria-hidden="true" className={cn("grid size-8 shrink-0 place-items-center rounded-md bg-muted text-foreground", off && "opacity-50")}>
      {Icon ? <Icon className="size-4" /> : icon as ReactElement}
    </span>
  )
}
