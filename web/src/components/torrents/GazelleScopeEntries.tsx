/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Link } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"

import { useGazelleLookupText } from "@/hooks/useGazelleLookupText"
import { gazelleSettingsReasons } from "@/lib/gazelle-lookup"
import { cn } from "@/lib/utils"
import type { CrossSeedGazelleLookup } from "@/types"

/** GazelleScopeEntries lists the RED/OPS sites of a search with the plan or the outcome of each. */
export function GazelleScopeEntries({ entries }: { entries: CrossSeedGazelleLookup[] }) {
  const { t } = useTranslation("torrents")
  const lookupText = useGazelleLookupText()
  if (entries.length === 0) {
    return null
  }
  return (
    <ul className="mt-2 space-y-0.5 text-xs" aria-label={t("crossSeedDialog.gazelle.heading")}>
      {entries.map(entry => (
        <li
          key={entry.site}
          data-status={entry.status}
          className={cn("flex min-w-0 flex-wrap items-center gap-x-1.5", entry.status === "not_asked" && "opacity-60")}
        >
          <span className="font-medium">{entry.name}</span>
          <span
            className={cn(
              "min-w-0 break-words",
              entry.status === "failed" ? "text-destructive" : entry.status === "match" ? "text-primary" : "text-muted-foreground"
            )}
          >
            {lookupText(entry)}
          </span>
          {entry.status === "not_asked" && gazelleSettingsReasons.has(entry.reason) && (
            <Link to="/settings" search={{ tab: "indexers" }} className="text-muted-foreground underline underline-offset-2 hover:text-foreground">
              {t("crossSeedDialog.gazelle.openSettings")}
            </Link>
          )}
        </li>
      ))}
    </ul>
  )
}
