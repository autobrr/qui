/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Checkbox } from "@/components/ui/checkbox"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger
} from "@/components/ui/context-menu"
import type { Category } from "@/types"
import { ChevronDown, ChevronRight, Edit, FolderPlus, Trash2 } from "lucide-react"
import { memo, useCallback, useMemo } from "react"
import type { MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent } from "react"

interface CategoryNode {
  name: string
  displayName: string
  category: Category
  children: CategoryNode[]
  parent?: CategoryNode
  level: number
  count: number
}

interface CategoryTreeProps {
  categories: Record<string, Category>
  counts: Record<string, number>
  useSubcategories: boolean
  collapsedCategories: Set<string>
  onToggleCollapse: (category: string) => void
  searchTerm?: string
  getCategoryState: (category: string) => "include" | "exclude" | "neutral"
  getCheckboxState: (state: "include" | "exclude" | "neutral") => boolean | "indeterminate"
  onCategoryCheckboxChange: (category: string) => void
  onCategoryPointerDown?: (event: ReactPointerEvent<HTMLElement>, category: string) => void
  onCreateSubcategory: (parent: string) => void
  onEditCategory: (category: string) => void
  onDeleteCategory: (category: string) => void
  onRemoveEmptyCategories?: () => void
  hasEmptyCategories?: boolean
  syntheticCategories?: Set<string>
  getCategoryCount: (category: string) => string
}

function buildCategoryTree(
  categories: Record<string, Category>,
  counts: Record<string, number>
): CategoryNode[] {
  const nodeMap = new Map<string, CategoryNode>()
  const roots: CategoryNode[] = []

  // First pass: create all nodes
  Object.entries(categories).forEach(([name, category]) => {
    const segments = name.split("/")
    const displayName = segments[segments.length - 1]

    const node: CategoryNode = {
      name,
      displayName,
      category,
      children: [],
      level: segments.length - 1,
      count: counts[`category:${name}`] || 0,
    }

    nodeMap.set(name, node)
  })

  // Second pass: build parent-child relationships
  nodeMap.forEach((node, name) => {
    const segments = name.split("/")

    if (segments.length === 1) {
      // Root category
      roots.push(node)
    } else {
      // Find parent
      const parentPath = segments.slice(0, -1).join("/")
      const parentNode = nodeMap.get(parentPath)

      if (parentNode) {
        parentNode.children.push(node)
        node.parent = parentNode
      } else {
        // Parent doesn't exist in categories, treat as root
        roots.push(node)
      }
    }
  })

  // Sort categories and their children
  const sortNodes = (nodes: CategoryNode[]) => {
    nodes.sort((a, b) => a.displayName.localeCompare(b.displayName))
    nodes.forEach(node => sortNodes(node.children))
  }

  sortNodes(roots)

  return roots
}

const CategoryTreeNode = memo(({
  node,
  getCategoryState,
  getCheckboxState,
  onCategoryCheckboxChange,
  onCategoryPointerDown,
  onCreateSubcategory,
  onEditCategory,
  onDeleteCategory,
  onRemoveEmptyCategories,
  hasEmptyCategories,
  collapsedCategories,
  onToggleCollapse,
  useSubcategories,
  syntheticCategories,
  getCategoryCount,
}: {
  node: CategoryNode
  getCategoryState: (category: string) => "include" | "exclude" | "neutral"
  getCheckboxState: (state: "include" | "exclude" | "neutral") => boolean | "indeterminate"
  onCategoryCheckboxChange: (category: string) => void
  onCategoryPointerDown?: (event: ReactPointerEvent<HTMLElement>, category: string) => void
  onCreateSubcategory: (parent: string) => void
  onEditCategory: (category: string) => void
  onDeleteCategory: (category: string) => void
  onRemoveEmptyCategories?: () => void
  hasEmptyCategories?: boolean
  collapsedCategories: Set<string>
  onToggleCollapse: (category: string) => void
  useSubcategories: boolean
  syntheticCategories?: Set<string>
  getCategoryCount: (category: string) => string
}) => {
  const hasChildren = node.children.length > 0
  const isCollapsed = collapsedCategories.has(node.name)
  const categoryState = getCategoryState(node.name)
  const checkboxState = getCheckboxState(categoryState)
  const indentLevel = node.level * 20
  const isSynthetic = syntheticCategories?.has(node.name) ?? false

  const handleToggleCollapse = useCallback((e: ReactMouseEvent<HTMLButtonElement>) => {
    e.stopPropagation()
    if (hasChildren) {
      onToggleCollapse(node.name)
    }
  }, [hasChildren, node.name, onToggleCollapse])

  const handleCheckboxChange = useCallback(() => {
    onCategoryCheckboxChange(node.name)
  }, [node.name, onCategoryCheckboxChange])

  const handlePointerDown = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    onCategoryPointerDown?.(event, node.name)
  }, [onCategoryPointerDown, node.name])

  const handleCreateSubcategory = useCallback(() => {
    if (isSynthetic) {
      return
    }
    onCreateSubcategory(node.name)
  }, [isSynthetic, node.name, onCreateSubcategory])

  const handleEditCategory = useCallback(() => {
    if (isSynthetic) {
      return
    }
    onEditCategory(node.name)
  }, [isSynthetic, node.name, onEditCategory])

  const handleDeleteCategory = useCallback(() => {
    if (isSynthetic) {
      return
    }
    onDeleteCategory(node.name)
  }, [isSynthetic, node.name, onDeleteCategory])

  return (
    <>
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <li
            className="flex items-center gap-2 px-2 py-1.5 hover:bg-accent rounded-md cursor-pointer select-none"
            style={{ paddingLeft: `${indentLevel + 8}px` }}
            onPointerDown={handlePointerDown}
            role="presentation"
          >
            {useSubcategories && (
              <button
                onClick={handleToggleCollapse}
                className={`size-4 flex items-center justify-center transition-opacity ${hasChildren ? "opacity-100" : "opacity-0 pointer-events-none"}`}
                type="button"
                aria-label={isCollapsed ? "Expand category" : "Collapse category"}
              >
                {isCollapsed ? (
                  <ChevronRight className="size-3" />
                ) : (
                  <ChevronDown className="size-3" />
                )}
              </button>
            )}

            <Checkbox
              checked={checkboxState}
              onCheckedChange={handleCheckboxChange}
              className="size-4"
            />

            <span
              className={`flex-1 text-sm cursor-pointer ${categoryState === "exclude" ? "text-destructive" : ""}`}
              onClick={handleCheckboxChange}
            >
              {node.displayName}
            </span>

            <span className={`text-xs ${categoryState === "exclude" ? "text-destructive" : "text-muted-foreground"}`}>
              ({getCategoryCount(node.name)})
            </span>
          </li>
        </ContextMenuTrigger>

        <ContextMenuContent>
          {useSubcategories && (
            <>
              <ContextMenuItem onClick={handleCreateSubcategory} disabled={isSynthetic}>
                <FolderPlus className="mr-2 size-4" />
                Create subcategory
              </ContextMenuItem>
              <ContextMenuSeparator />
            </>
          )}
          <ContextMenuItem onClick={handleEditCategory} disabled={isSynthetic}>
            <Edit className="mr-2 size-4" />
            Edit category
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem onClick={handleDeleteCategory} disabled={isSynthetic} className="text-destructive">
            <Trash2 className="mr-2 size-4" />
            Delete category
          </ContextMenuItem>
          {onRemoveEmptyCategories && (
            <ContextMenuItem
              onClick={() => onRemoveEmptyCategories()}
              disabled={!hasEmptyCategories}
              className="text-destructive"
            >
              <Trash2 className="mr-2 size-4" />
              Remove Empty Categories
            </ContextMenuItem>
          )}
        </ContextMenuContent>
      </ContextMenu>

      {useSubcategories && hasChildren && !isCollapsed && (
        <ul>
          {node.children.map((child) => (
            <CategoryTreeNode
              key={child.name}
              node={child}
              getCategoryState={getCategoryState}
              getCheckboxState={getCheckboxState}
              onCategoryCheckboxChange={onCategoryCheckboxChange}
              onCategoryPointerDown={onCategoryPointerDown}
              onCreateSubcategory={onCreateSubcategory}
              onEditCategory={onEditCategory}
              onDeleteCategory={onDeleteCategory}
              onRemoveEmptyCategories={onRemoveEmptyCategories}
              hasEmptyCategories={hasEmptyCategories}
              collapsedCategories={collapsedCategories}
              onToggleCollapse={onToggleCollapse}
              useSubcategories={useSubcategories}
              syntheticCategories={syntheticCategories}
              getCategoryCount={getCategoryCount}
            />
          ))}
        </ul>
      )}
    </>
  )
})

CategoryTreeNode.displayName = "CategoryTreeNode"

export const CategoryTree = memo(({
  categories,
  counts,
  useSubcategories,
  getCategoryState,
  getCheckboxState,
  onCategoryCheckboxChange,
  onCategoryPointerDown,
  onCreateSubcategory,
  onEditCategory,
  onDeleteCategory,
  onRemoveEmptyCategories,
  hasEmptyCategories = false,
  collapsedCategories,
  onToggleCollapse,
  searchTerm = "",
  syntheticCategories = new Set<string>(),
  getCategoryCount,
}: CategoryTreeProps) => {
  // Filter categories based on search term
  const filteredCategories = useMemo(() => {
    if (!searchTerm) return categories

    const searchLower = searchTerm.toLowerCase()
    return Object.fromEntries(
      Object.entries(categories).filter(([name]) =>
        name.toLowerCase().includes(searchLower)
      )
    )
  }, [categories, searchTerm])

  // Build flat list for non-subcategory mode
  const flatCategories = Object.entries(filteredCategories).map(([name, category]) => ({
    name,
    displayName: name,
    category,
    children: [],
    level: 0,
    count: counts[`category:${name}`] || 0,
  })).sort((a, b) => a.name.localeCompare(b.name))

  // Build tree for subcategory mode
  const categoryTree = useSubcategories? buildCategoryTree(filteredCategories, counts): flatCategories
  const uncategorizedState = getCategoryState("")
  const uncategorizedCheckboxState = getCheckboxState(uncategorizedState)
  const uncategorizedCount = getCategoryCount("")

  return (
    <div className="space-y-1">
      {/* All/Uncategorized special items */}

      <li
        className="flex items-center gap-2 px-2 py-1.5 hover:bg-accent rounded-md cursor-pointer"
        onClick={() => onCategoryCheckboxChange("")}
        onPointerDown={(event) => onCategoryPointerDown?.(event, "")}
      >
        <Checkbox
          checked={uncategorizedCheckboxState}
          className="size-4"
        />
        <span className={`flex-1 text-sm italic ${uncategorizedState === "exclude" ? "text-destructive" : "text-muted-foreground"}`}>
          Uncategorized
        </span>
        <span className={`text-xs ${uncategorizedState === "exclude" ? "text-destructive" : "text-muted-foreground"}`}>
          ({uncategorizedCount})
        </span>
      </li>

      <div className="border-t my-2" />

      {/* Category tree/list */}
      {categoryTree.map((node) => (
        <CategoryTreeNode
          key={node.name}
          node={node}
          getCategoryState={getCategoryState}
          getCheckboxState={getCheckboxState}
          onCategoryCheckboxChange={onCategoryCheckboxChange}
          onCategoryPointerDown={onCategoryPointerDown}
          onCreateSubcategory={onCreateSubcategory}
          onEditCategory={onEditCategory}
          onDeleteCategory={onDeleteCategory}
          onRemoveEmptyCategories={onRemoveEmptyCategories}
          hasEmptyCategories={hasEmptyCategories}
          collapsedCategories={collapsedCategories}
          onToggleCollapse={onToggleCollapse}
          useSubcategories={useSubcategories}
          syntheticCategories={syntheticCategories}
          getCategoryCount={getCategoryCount}
        />
      ))}
    </div>
  )
})

CategoryTree.displayName = "CategoryTree"
