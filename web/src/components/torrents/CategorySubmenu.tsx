/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
import { useVirtualizer } from "@tanstack/react-virtual"
import { Folder, Search, X } from "lucide-react"
import { memo, useDeferredValue, useMemo, useRef, useState } from "react"
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

// Threshold for when to use virtualization vs simple rendering
// Below this, simple CSS scrolling is faster
const VIRTUALIZATION_THRESHOLD = 250

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
  // Use deferred value to prevent search from blocking the UI
  const deferredSearchQuery = useDeferredValue(searchQuery)
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  const SubTrigger = type === "context" ? ContextMenuSubTrigger : DropdownMenuSubTrigger
  const Sub = type === "context" ? ContextMenuSub : DropdownMenuSub
  const SubContent = type === "context" ? ContextMenuSubContent : DropdownMenuSubContent
  const MenuItem = type === "context" ? ContextMenuItem : DropdownMenuItem
  const Separator = type === "context" ? ContextMenuSeparator : DropdownMenuSeparator

  const hasCategories = Object.keys(availableCategories).length > 0
  const categoryCount = Object.keys(availableCategories).length

  // Use deferred value for filtering to prevent blocking
  const filteredCategories = useMemo(() => {
    const query = deferredSearchQuery.trim().toLowerCase()

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
    const namesFiltered = query ? names.filter(cat => cat.toLowerCase().includes(query)) : names

    return namesFiltered.map((name) => ({
      name,
      displayName: name,
      level: 0,
    }))
  }, [availableCategories, deferredSearchQuery, useSubcategories])

  const hasFilteredCategories = filteredCategories.length > 0
  const shouldUseVirtualization = categoryCount > VIRTUALIZATION_THRESHOLD

  // Only initialize virtualizer if we need it
  const virtualizer = useVirtualizer({
    count: shouldUseVirtualization ? filteredCategories.length : 0,
    getScrollElement: () => scrollContainerRef.current,
    estimateSize: () => 36,
    overscan: 5,
  })

  // Render a single category item (shared between virtualized and non-virtualized)
  const renderCategoryItem = (category: { name: string; displayName: string; level: number }) => (
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
  )

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

            {/* Search bar - only show if there are many categories */}
            {categoryCount > 10 && (
              <>
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
          </>
        )}

        {/* Category list - use virtualization only for large lists */}
        {hasCategories && (
          <div
            ref={scrollContainerRef}
            className="max-h-[300px] overflow-y-auto"
          >
            {hasFilteredCategories ? (
              shouldUseVirtualization ? (
                // Virtualized rendering for large lists
                <div
                  style={{
                    height: `${virtualizer.getTotalSize()}px`,
                    width: "100%",
                    position: "relative",
                  }}
                >
                  {virtualizer.getVirtualItems().map((virtualRow) => {
                    const category = filteredCategories[virtualRow.index]
                    return (
                      <div
                        key={virtualRow.key}
                        data-index={virtualRow.index}
                        ref={virtualizer.measureElement}
                        style={{
                          position: "absolute",
                          top: 0,
                          left: 0,
                          width: "100%",
                          transform: `translateY(${virtualRow.start}px)`,
                        }}
                      >
                        {renderCategoryItem(category)}
                      </div>
                    )
                  })}
                </div>
              ) : (
                // Simple rendering for smaller lists - much faster!
                <div className="py-1">
                  {filteredCategories.map((category) => renderCategoryItem(category))}
                </div>
              )
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

CategorySubmenu.displayName = "CategorySubmenu"
