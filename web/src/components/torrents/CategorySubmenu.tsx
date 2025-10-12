/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger
} from "@/components/ui/context-menu"
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Folder, Search, X } from "lucide-react"
import { memo, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

interface CategorySubmenuProps {
  type: "context" | "dropdown"
  hashCount: number
  availableCategories: Record<string, unknown>
  onSetCategory: (category: string) => void
  isPending?: boolean
  currentCategory?: string
}

export const CategorySubmenu = memo(function CategorySubmenu({
  type,
  hashCount,
  availableCategories,
  onSetCategory,
  isPending = false,
  currentCategory,
}: CategorySubmenuProps) {
  const { t } = useTranslation()
  const [searchQuery, setSearchQuery] = useState("")

  const SubTrigger = type === "context" ? ContextMenuSubTrigger : DropdownMenuSubTrigger
  const Sub = type === "context" ? ContextMenuSub : DropdownMenuSub
  const SubContent = type === "context" ? ContextMenuSubContent : DropdownMenuSubContent
  const MenuItem = type === "context" ? ContextMenuItem : DropdownMenuItem
  const Separator = type === "context" ? ContextMenuSeparator : DropdownMenuSeparator

  const categories = Object.keys(availableCategories || {}).sort()
  const hasCategories = categories.length > 0

  // Filter categories based on search query
  const filteredCategories = useMemo(() => {
    if (!searchQuery.trim()) return categories
    const query = searchQuery.toLowerCase()
    return categories.filter(cat => cat.toLowerCase().includes(query))
  }, [categories, searchQuery])

  return (
    <Sub>
      <SubTrigger disabled={isPending}>
        <Folder className="mr-4 h-4 w-4" />
        {t("torrent_context_menu.category.set_category")}
      </SubTrigger>
      <SubContent className="p-0 min-w-[240px]">
        {/* Remove Category option */}
        <MenuItem
          onClick={() => onSetCategory("")}
          disabled={isPending}
        >
          <X className="mr-2 h-4 w-4" />
          <span className="text-muted-foreground italic">
            {t("torrent_context_menu.category.no_category", {
              count: hashCount,
            })}
          </span>
        </MenuItem>

        {hasCategories && (
          <>
            <Separator />

            {/* Search bar */}
            <div className="p-2" onClick={(e) => e.stopPropagation()}>
              <div className="relative">
                <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
                <Input
                  placeholder={t(
                    "torrent_context_menu.category.search_placeholder"
                  )}
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  onKeyDown={(e) => e.stopPropagation()}
                  className="h-8 pl-8"
                  autoFocus={false}
                />
              </div>
            </div>

            <Separator />
          </>
        )}

        {/* Scrollable category list */}
        {hasCategories && (
          <div className="max-h-[300px] overflow-y-auto">
            {filteredCategories.length > 0 ? (
              filteredCategories.map((category) => (
                <MenuItem
                  key={category}
                  onClick={() => onSetCategory(category)}
                  disabled={isPending}
                  className={currentCategory === category ? "bg-accent" : ""}
                >
                  <Folder className="mr-2 h-4 w-4" />
                  {t("torrent_context_menu.category.category_item", {
                    category,
                    count: hashCount,
                  })}
                </MenuItem>
              ))
            ) : (
              <div className="px-2 py-6 text-center text-sm text-muted-foreground">
                {t("torrent_context_menu.category.no_categories_found")}
              </div>
            )}
          </div>
        )}

        {/* Creating new categories from this menu is disabled. */}
      </SubContent>
    </Sub>
  )
})