/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Link } from "@tanstack/react-router"
import { AlertTriangle } from "lucide-react"
import { useTranslation } from "react-i18next"

import { Checkbox } from "@/components/ui/checkbox"
import { cn } from "@/lib/utils"

export type ScopeEntry = {
  key: string
  name: string
  status: string
  picked: boolean | null
  note?: string
  noteClass?: string
  settingsLink?: boolean
  attention?: boolean
  onToggle?: () => void
}

type SearchScopeEntriesProps = {
  entries: ScopeEntry[]
  disabled: boolean
}

export function SearchScopeEntries({ entries, disabled }: SearchScopeEntriesProps) {
  const { t } = useTranslation("torrents")
  const pickable = entries.filter(entry => entry.picked !== null)
  const skipped = entries.filter(entry => entry.picked === null)
  return (
    <>
      {pickable.length > 0 && (
        <ul className="grid grid-cols-1 gap-x-4 sm:grid-cols-2" aria-label={t("crossSeedDialog.scope.heading")}>
          {pickable.map(entry => (
            <li key={entry.key} data-status={entry.status} className="min-w-0">
              <label
                className={cn(
                  "-mx-2 flex min-h-11 min-w-0 cursor-pointer items-center gap-3 rounded-md px-2 py-1.5 text-sm transition-colors hover:bg-accent/40 sm:min-h-8 sm:gap-2.5 sm:py-1 sideways:min-h-11 sideways:gap-3 sideways:py-1.5",
                  disabled && "cursor-not-allowed opacity-60"
                )}
              >
                <Checkbox checked={Boolean(entry.picked)} disabled={disabled} onCheckedChange={entry.onToggle} className="shrink-0" />
                <span className="min-w-0 flex-1">
                  <span className="flex min-w-0 items-center gap-1.5">
                    <span className={cn("min-w-0 break-words", !entry.picked && "text-muted-foreground")}>{entry.name}</span>
                    {(entry.status === "error" || entry.status === "failed" || entry.status === "not_covered") && (
                      <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-yellow-600 dark:text-yellow-500" aria-hidden />
                    )}
                  </span>
                  {entry.note && <span className={cn("block break-words text-xs", entry.noteClass ?? "text-muted-foreground")}>{entry.note}</span>}
                </span>
              </label>
            </li>
          ))}
        </ul>
      )}
      {skipped.length > 0 && (
        <ul className="space-y-0.5 text-xs text-muted-foreground">
          {skipped.map(entry => (
            <li key={entry.key} data-status={entry.status} className="min-w-0 break-words">
              <span className="font-medium text-foreground/80">{entry.name}</span>
              {entry.note && <span>{" · "}{entry.note}</span>}
              {entry.settingsLink && (
                <>
                  {" · "}
                  <Link to="/settings" search={{ tab: "indexers" }} className="-my-3 inline-block py-3 underline underline-offset-2 hover:text-foreground sm:my-0 sm:py-0 sideways:-my-3 sideways:py-3">
                    {t("crossSeedDialog.gazelle.openSettings")}
                  </Link>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
    </>
  )
}
