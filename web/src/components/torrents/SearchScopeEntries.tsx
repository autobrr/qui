/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Link } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"

import { useGazelleLookupText } from "@/hooks/useGazelleLookupText"
import { gazelleSettingsReasons, gazelleStatusClass } from "@/lib/gazelle-lookup"
import { cn } from "@/lib/utils"
import type { CrossSeedGazelleLookup } from "@/types"

export type TorznabScopeEntry = { id: number; name: string; asked: boolean }

type SearchScopeEntriesProps = {
  torznab: TorznabScopeEntry[]
  gazelle: CrossSeedGazelleLookup[]
}

/**
 * SearchScopeEntries lists each Torznab indexer and each RED/OPS site of a
 * search. An entry that the search does not ask is grey. A RED/OPS entry also
 * shows its plan or its outcome.
 */
export function SearchScopeEntries({ torznab, gazelle }: SearchScopeEntriesProps) {
  const { t } = useTranslation("torrents")
  const lookupText = useGazelleLookupText()
  if (torznab.length === 0 && gazelle.length === 0) {
    return null
  }
  const entryClass = "inline-flex min-w-0 max-w-full flex-wrap items-center gap-x-1 rounded border border-border/60 bg-background/40 px-1.5 py-0.5"
  return (
    <ul className="mt-2 flex flex-wrap gap-1 text-xs" aria-label={t("crossSeedDialog.searchScope")}>
      {torznab.map(entry => (
        <li key={entry.id} data-status={entry.asked ? "will_ask" : "not_asked"} className={cn(entryClass, !entry.asked && "opacity-50")}>
          <span className="min-w-0 truncate">{entry.name}</span>
        </li>
      ))}
      {gazelle.map(entry => (
        <li key={entry.site} data-status={entry.status} className={cn(entryClass, entry.status === "not_asked" && "opacity-60")}>
          <span className="font-medium">{entry.name}</span>
          <span className={cn("min-w-0 break-words", gazelleStatusClass(entry.status))}>{lookupText(entry)}</span>
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
