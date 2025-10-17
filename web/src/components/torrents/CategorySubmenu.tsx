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
import { cn } from "@/lib/utils"
import type { Category } from "@/types"
import { Folder, Search, X } from "lucide-react"
import { memo, useMemo, useState } from "react"
import { buildCategoryTree, type CategoryNode } from "./CategoryTree"

interface CategorySubmenuProps {
  type: "context" | "dropdown"
  hashCount: number
  availableCategories: Record<string, Category>
  onSetCategory: (category: string) => void
  isPending?: boolean
  currentCategory?: string
  useSubcategories?: boolean
}

export const CategorySubmenu = memo(function CategorySubmenu({
  type,
  hashCount,
  availableCategories,
  onSetCategory,
  isPending = false,
  currentCategory,
  useSubcategories = false,
}: CategorySubmenuProps) {
  const [searchQuery, setSearchQuery] = useState("")

  const SubTrigger = type === "context" ? ContextMenuSubTrigger : DropdownMenuSubTrigger
  const Sub = type === "context" ? ContextMenuSub : DropdownMenuSub
  const SubContent = type === "context" ? ContextMenuSubContent : DropdownMenuSubContent
  const MenuItem = type === "context" ? ContextMenuItem : DropdownMenuItem
  const Separator = type === "context" ? ContextMenuSeparator : DropdownMenuSeparator

  const hasCategories = Object.keys(availableCategories).length > 0

  const filteredCategories = useMemo(() => {
    const query = searchQuery.trim().toLowerCase()

    if (useSubcategories) {
      const tree = buildCategoryTree(availableCategories, {})
      const shouldIncludeCache = new Map<CategoryNode, boolean>()

      const shouldIncludeNode = (node: CategoryNode): boolean => {
        const cached = shouldIncludeCache.get(node)
        if (cached !== undefined) {
          return cached
        }

        const nodeMatches = query === "" || node.name.toLowerCase().includes(query)
        if (nodeMatches) {
          shouldIncludeCache.set(node, true)
          return true
        }

        for (const child of node.children) {
          if (shouldIncludeNode(child)) {
            shouldIncludeCache.set(node, true)
            return true
          }
        }

        shouldIncludeCache.set(node, false)
        return false
      }

      const flattened: Array<{ name: string; displayName: string; level: number }> = []

      const visitNodes = (nodes: CategoryNode[]) => {
        for (const node of nodes) {
          if (shouldIncludeNode(node)) {
            flattened.push({
              name: node.name,
              displayName: node.displayName,
              level: node.level,
            })
            visitNodes(node.children)
          }
        }
      }

      visitNodes(tree)
      return flattened
    }

    const names = Object.keys(availableCategories).sort()
    const namesFiltered = query? names.filter(cat => cat.toLowerCase().includes(query)): names

    return namesFiltered.map((name) => ({
      name,
      displayName: name,
      level: 0,
    }))
  }, [availableCategories, searchQuery, useSubcategories])

  const hasFilteredCategories = filteredCategories.length > 0

  return (
    <Sub>
      <SubTrigger disabled={isPending}>
        <Folder className="mr-4 h-4 w-4" />
        Set Category
      </SubTrigger>
      <SubContent className="p-0 min-w-[240px]">
        {/* Remove Category option */}
        <MenuItem
          onClick={() => onSetCategory("")}
          disabled={isPending}
        >
          <X className="mr-2 h-4 w-4" />
          <span className="text-muted-foreground italic">
            (No category) {hashCount > 1 ? `(${hashCount})` : ""}
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
                  placeholder="Search categories..."
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
            {hasFilteredCategories ? (
              filteredCategories.map((category) => (
                <MenuItem
                  key={category.name}
                  onClick={() => onSetCategory(category.name)}
                  disabled={isPending}
                  className={cn(
                    "flex items-center gap-2",
                    currentCategory === category.name ? "bg-accent" : ""
                  )}
                >
                  <Folder className="mr-2 h-4 w-4" />
                  <span
                    className="flex-1 truncate"
                    title={category.name}
                    style={category.level > 0 ? { paddingLeft: category.level * 12 } : undefined}
                  >
                    {category.displayName}
                  </span>
                  {hashCount > 1 && (
                    <span className="text-xs text-muted-foreground">
                      ({hashCount})
                    </span>
                  )}
                </MenuItem>
              ))
            ) : (
              <div className="px-2 py-6 text-center text-sm text-muted-foreground">
                No categories found
              </div>
            )}
          </div>
        )}

        {/* Creating new categories from this menu is disabled. */}
      </SubContent>
    </Sub>
  )
})
