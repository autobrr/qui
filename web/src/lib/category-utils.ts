/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { buildCategoryTree, type CategoryNode } from "@/components/torrents/CategoryTree"

/** Build category select options from categories object, preserving any manually-typed selections */
export function buildCategorySelectOptions(
  categories: Record<string, { name: string; savePath: string }>,
  ...selectedArrays: string[][]
): Array<{ label: string; value: string }> {
  const tree = buildCategoryTree(categories, {})
  const flattened: { label: string; value: string }[] = []

  const visitNodes = (nodes: CategoryNode[]) => {
    for (const node of nodes) {
      flattened.push({ label: node.name, value: node.name })
      visitNodes(node.children)
    }
  }
  visitNodes(tree)

  // Add any extras from selected arrays that aren't in the tree
  const allSelected = selectedArrays.flat()
  for (const cat of allSelected) {
    if (!flattened.some(opt => opt.value === cat)) {
      flattened.push({ label: cat, value: cat })
    }
  }

  return flattened
}

/** Build tag select options from available tags, preserving any manually-typed selections */
export function buildTagSelectOptions(
  availableTags: string[],
  ...selectedArrays: string[][]
): Array<{ label: string; value: string }> {
  const allTags = new Set([...availableTags, ...selectedArrays.flat()])
  return Array.from(allTags).map(tag => ({ label: tag, value: tag }))
}
