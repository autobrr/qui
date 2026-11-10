/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMediaQuery } from "@/hooks/useMediaQuery"
import { cn } from "@/lib/utils"
import { AlertTriangle, ChevronDown } from "lucide-react"
import { useTranslation } from "react-i18next"

export type CopyGroupPillSize = "normal" | "dense" | "stacked"

interface CopyGroupPillProps {
  id: string
  open: boolean
  copies: number
  /** Only on a collapsed group: a hidden copy has a problem. */
  hiddenProblem: boolean
  /** Only on a collapsed group outside Select all mode: the ticked copies that it hides. */
  hiddenTicks: number
  /** The name that the row shows, so each pill has its own accessible name. */
  name: string
  size: CopyGroupPillSize
  onToggle: (id: string) => void
}

const HEIGHT: Record<CopyGroupPillSize, string> = { normal: "h-5", dense: "h-[18px]", stacked: "h-[22px]" }

// The "+N" toggle at the end of the name of a Copy group's top row. The name
// stays the same in both states: aria-expanded tells open from closed.
export function CopyGroupPill({ id, open, copies, hiddenProblem, hiddenTicks, name, size, onToggle }: CopyGroupPillProps) {
  const { t } = useTranslation("torrents")
  const canHover = useMediaQuery("(hover: hover)")
  const copiesLabel = hiddenProblem ? t("copyGroups.copiesWithProblem", { count: copies, name }) : t("copyGroups.copies", { count: copies, name })
  const label = hiddenTicks > 0 ? t("copyGroups.labelWithTicks", { label: copiesLabel, ticks: t("copyGroups.hiddenTicked", { count: hiddenTicks }) }) : copiesLabel
  return (
    // The 18px pill takes 16px of its line, so it does not make the smallest card taller.
    <span className={cn("ml-auto flex shrink-0 pl-2", size === "stacked" && "mr-2", size === "dense" && "-my-px")}>
      <button
        type="button"
        aria-expanded={open}
        aria-label={label}
        onClick={(event) => {
          event.stopPropagation()
          onToggle(id)
        }}
        // A press on the pill must not start the long press of its card.
        onTouchStart={(event) => event.stopPropagation()}
        className={cn(
          "relative flex cursor-pointer items-center gap-0.5 rounded-md border pl-1.5 pr-1 text-xs font-medium tabular-nums transition-colors",
          HEIGHT[size],
          // An invisible hit area 5px past the border on each side. A touch screen
          // gets the full height: the zone is taller than any row, and the row or the card clips it.
          "before:absolute before:-inset-x-[5px] before:content-['']",
          canHover ? "before:-inset-y-[5px]" : "before:-inset-y-48",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:ring-offset-background",
          open? "border-foreground/70 bg-transparent text-foreground hover:bg-muted": hiddenProblem? "border-destructive/50 bg-destructive/10 text-foreground hover:border-destructive hover:bg-destructive/20": "border-border bg-secondary text-secondary-foreground hover:border-foreground/30 hover:bg-muted"
        )}
      >
        {hiddenProblem && <AlertTriangle className="mr-0.5 h-3.5 w-3.5 text-destructive" aria-hidden="true" />}
        <span>{hiddenTicks > 0 ? t("copyGroups.badgeTicked", { hidden: copies, count: hiddenTicks }) : t("copyGroups.badge", { hidden: copies })}</span>
        <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", open && "rotate-180")} aria-hidden="true" />
      </button>
    </span>
  )
}
