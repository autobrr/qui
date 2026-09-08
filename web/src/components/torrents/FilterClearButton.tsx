/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { usePersistedColumnFilters } from "@/hooks/usePersistedColumnFilters"
import { navigateWithSearch } from "@/lib/router-search"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { ChevronDown } from "lucide-react"
import { useTranslation } from "react-i18next"

interface FilterClearButtonProps {
  instanceId: number
  hasSidebarFilters: boolean
  onClearSidebar: () => void
}

export function FilterClearButton({ instanceId, hasSidebarFilters, onClearSidebar }: FilterClearButtonProps) {
  const { t } = useTranslation("torrents")
  const [columnFilters, setColumnFilters] = usePersistedColumnFilters(instanceId)
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as { q?: string; [key: string]: unknown }

  const clearFilters = () => {
    onClearSidebar()
    setColumnFilters([])
  }

  if (!hasSidebarFilters && columnFilters.length === 0) return null

  return (
    <div className="flex shrink-0 items-center text-xs text-muted-foreground">
      <button
        type="button"
        onClick={clearFilters}
        className="rounded-l px-1 py-1 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        {t("columnFilter.clearFilters")}
      </button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className="rounded-r border-l px-1 py-1 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            aria-label={t("filterSidebar.clearOptions")}
          >
            <ChevronDown className="size-3.5" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem disabled={!hasSidebarFilters} onSelect={onClearSidebar}>
            {t("filterSidebar.clearSidebarFilters")}
          </DropdownMenuItem>
          <DropdownMenuItem disabled={columnFilters.length === 0} onSelect={() => setColumnFilters([])}>
            {t("filterSidebar.clearColumnFilters")}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            disabled={!search.q}
            onSelect={() => {
              clearFilters()
              const next = { ...search }
              delete next.q
              navigateWithSearch({ navigate, search: next, replace: true })
            }}
          >
            {t("filterSidebar.clearFiltersAndSearch")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
