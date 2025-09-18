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
  selectedCategories: string[]
  useSubcategories: boolean
  onCategoryToggle: (category: string) => void
  onCreateSubcategory: (parent: string) => void
  onEditCategory: (category: string) => void
  onDeleteCategory: (category: string) => void
  collapsedCategories: Set<string>
  onToggleCollapse: (category: string) => void
  searchTerm?: string
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
  selectedCategories,
  onCategoryToggle,
  onCreateSubcategory,
  onEditCategory,
  onDeleteCategory,
  collapsedCategories,
  onToggleCollapse,
  useSubcategories,
}: {
  node: CategoryNode
  selectedCategories: string[]
  onCategoryToggle: (category: string) => void
  onCreateSubcategory: (parent: string) => void
  onEditCategory: (category: string) => void
  onDeleteCategory: (category: string) => void
  collapsedCategories: Set<string>
  onToggleCollapse: (category: string) => void
  useSubcategories: boolean
}) => {
  const hasChildren = node.children.length > 0
  const isCollapsed = collapsedCategories.has(node.name)
  const isSelected = selectedCategories.includes(node.name)
  const indentLevel = node.level * 20

  const handleToggleCollapse = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    if (hasChildren) {
      onToggleCollapse(node.name)
    }
  }, [hasChildren, node.name, onToggleCollapse])

  const handleCategoryClick = useCallback(() => {
    onCategoryToggle(node.name)
  }, [node.name, onCategoryToggle])

  return (
    <>
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <li
            className="flex items-center gap-2 px-2 py-1.5 hover:bg-accent rounded-md cursor-pointer select-none"
            style={{ paddingLeft: `${indentLevel + 8}px` }}
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
              checked={isSelected}
              onCheckedChange={handleCategoryClick}
              className="size-4"
            />

            <span
              className="flex-1 text-sm cursor-pointer"
              onClick={handleCategoryClick}
            >
              {node.displayName}
            </span>

            <span className="text-xs text-muted-foreground">
              ({node.count})
            </span>
          </li>
        </ContextMenuTrigger>

        <ContextMenuContent>
          {useSubcategories && (
            <>
              <ContextMenuItem onClick={() => onCreateSubcategory(node.name)}>
                <FolderPlus className="mr-2 size-4" />
                Create subcategory
              </ContextMenuItem>
              <ContextMenuSeparator />
            </>
          )}
          <ContextMenuItem onClick={() => onEditCategory(node.name)}>
            <Edit className="mr-2 size-4" />
            Edit category
          </ContextMenuItem>
          <ContextMenuItem onClick={() => onDeleteCategory(node.name)}>
            <Trash2 className="mr-2 size-4" />
            Delete category
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>

      {useSubcategories && hasChildren && !isCollapsed && (
        <ul>
          {node.children.map((child) => (
            <CategoryTreeNode
              key={child.name}
              node={child}
              selectedCategories={selectedCategories}
              onCategoryToggle={onCategoryToggle}
              onCreateSubcategory={onCreateSubcategory}
              onEditCategory={onEditCategory}
              onDeleteCategory={onDeleteCategory}
              collapsedCategories={collapsedCategories}
              onToggleCollapse={onToggleCollapse}
              useSubcategories={useSubcategories}
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
  selectedCategories,
  useSubcategories,
  onCategoryToggle,
  onCreateSubcategory,
  onEditCategory,
  onDeleteCategory,
  collapsedCategories,
  onToggleCollapse,
  searchTerm = "",
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

  return (
    <div className="space-y-1">
      {/* All/Uncategorized special items */}

      <li
        className="flex items-center gap-2 px-2 py-1.5 hover:bg-accent rounded-md cursor-pointer"
        onClick={() => onCategoryToggle("")}
      >
        <Checkbox
          checked={selectedCategories.includes("")}
          className="size-4"
        />
        <span className="flex-1 text-sm">Uncategorized</span>
        <span className="text-xs text-muted-foreground">
          ({counts["category:"] || 0})
        </span>
      </li>

      <div className="border-t my-2" />

      {/* Category tree/list */}
      {categoryTree.map((node) => (
        <CategoryTreeNode
          key={node.name}
          node={node}
          selectedCategories={selectedCategories}
          onCategoryToggle={onCategoryToggle}
          onCreateSubcategory={onCreateSubcategory}
          onEditCategory={onEditCategory}
          onDeleteCategory={onDeleteCategory}
          collapsedCategories={collapsedCategories}
          onToggleCollapse={onToggleCollapse}
          useSubcategories={useSubcategories}
        />
      ))}
    </div>
  )
})

CategoryTree.displayName = "CategoryTree"