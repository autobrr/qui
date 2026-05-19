/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TreeViewElement } from "./file-tree"

export const pathsToTreeView = (
  paths: string[],
  {
    selectablePaths,
    delimiter = "/",
    leafType = "file",
  }: {
    selectablePaths?: Set<string>
    delimiter?: string
    leafType?: "file" | "folder",
  } = {}
): TreeViewElement[] => {
  const uniquePaths = Array.from(new Set(paths.filter(Boolean))).sort((a, b) => a.localeCompare(b))
  const nodes = new Map<string, TreeViewElement>()
  const roots: TreeViewElement[] = []

  uniquePaths.forEach(fullPath => {
    const segments = fullPath.split(delimiter).filter(Boolean)
    let parentPath = ""

    segments.forEach((segment, index) => {
      const currentPath = parentPath ? `${parentPath}${delimiter}${segment}` : segment
      const isLeaf = index === segments.length - 1
      let node = nodes.get(currentPath)

      if (!node) {
        node = {
          id: currentPath,
          name: segment,
          kind: isLeaf ? leafType : "folder",
          isSelectable: selectablePaths?.has(currentPath) ?? false,
        }
        nodes.set(currentPath, node)

        if (parentPath) {
          const parentNode = nodes.get(parentPath)
          if (parentNode) {
            parentNode.kind = "folder"
            parentNode.children = parentNode.children ?? []
            if (!parentNode.children.some(child => child.id === currentPath)) {
              parentNode.children.push(node)
            }
          }
        } else {
          roots.push(node)
        }
      } else {
        if (!isLeaf) {
          node.kind = "folder"
          node.children = node.children ?? []
        }
        if (selectablePaths?.has(currentPath)) {
          node.isSelectable = true
        }
      }

      parentPath = currentPath
    })
  })

  const sortNodes = (items: TreeViewElement[]) => {
    items.sort((a, b) => a.name.localeCompare(b.name))
    items.forEach(item => {
      if (item.children) {
        sortNodes(item.children)
      }
    })
  }

  sortNodes(roots)

  return roots
}
