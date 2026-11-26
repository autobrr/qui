/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useIsPhone } from "@/hooks/useMediaQuery"
import { cn } from "@/lib/utils"
import { Loader2, MoreHorizontal, type LucideIcon } from "lucide-react"
import { Fragment, type ComponentProps } from "react"
import { useTranslation } from "react-i18next"

export interface RowAction {
  label: string
  icon: LucideIcon
  onSelect?: () => void
  href?: string
  destructive?: boolean
  pending?: boolean
  disabled?: boolean
}

interface RowActionsProps {
  actions: readonly RowAction[]
  describedBy: string
}

export function RowActions({ actions, describedBy }: RowActionsProps) {
  const { t } = useTranslation("common")
  const phone = useIsPhone()

  if (!phone) {
    return (
      <div className="flex items-center justify-end">
        {actions.map(action => (
          <Tooltip key={action.label}>
            <TooltipTrigger asChild>
              <ActionButton action={action} describedBy={describedBy} className="size-8" />
            </TooltipTrigger>
            <TooltipContent>{action.label}</TooltipContent>
          </Tooltip>
        ))}
      </div>
    )
  }

  if (actions.length === 1) {
    return <ActionButton action={actions[0]} describedBy={describedBy} className="size-11" />
  }

  const menu = [...actions.filter(action => !action.destructive), ...actions.filter(action => action.destructive)]
  const firstDestructive = menu.findIndex(action => action.destructive)
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={t("kit.moreActions")} aria-describedby={describedBy} className="size-11 text-muted-foreground">
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-44">
        {menu.map((action, index) => (
          <Fragment key={action.label}>
            {index === firstDestructive && index > 0 && <DropdownMenuSeparator />}
            <DropdownMenuItem
              asChild={linkOf(action) !== undefined}
              variant={action.destructive ? "destructive" : "default"}
              disabled={action.pending || action.disabled}
              onSelect={action.onSelect}
              className="pointer-coarse:py-2.5"
            >
              {linkOf(action) !== undefined ? (
                <a href={linkOf(action)} download>
                  <ActionIcon action={action} />
                  {action.label}
                </a>
              ) : (
                <>
                  <ActionIcon action={action} />
                  {action.label}
                </>
              )}
            </DropdownMenuItem>
          </Fragment>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

interface ActionButtonProps extends ComponentProps<typeof Button> {
  action: RowAction
  describedBy: string
}

function ActionButton({ action, describedBy, className, onClick, ...props }: ActionButtonProps) {
  const classes = cn("text-muted-foreground hover:text-foreground", action.destructive && "hover:text-destructive", className)
  const href = linkOf(action)
  if (href !== undefined) {
    return (
      <Button {...props} onClick={onClick} asChild variant="ghost" size="icon" className={classes}>
        <a href={href} download aria-label={action.label} aria-describedby={describedBy}>
          <ActionIcon action={action} />
        </a>
      </Button>
    )
  }
  return (
    <Button
      {...props}
      variant="ghost"
      size="icon"
      aria-label={action.label}
      aria-describedby={describedBy}
      disabled={action.pending || action.disabled}
      onClick={(event) => {
        onClick?.(event)
        action.onSelect?.()
      }}
      className={classes}
    >
      <ActionIcon action={action} />
    </Button>
  )
}

function linkOf(action: RowAction): string | undefined {
  return action.pending || action.disabled ? undefined : action.href
}

function ActionIcon({ action }: { action: RowAction }) {
  return action.pending ? <Loader2 className="animate-spin" /> : <action.icon />
}
