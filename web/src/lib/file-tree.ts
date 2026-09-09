/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FILE_PRIORITY, foldFolderPriority, normalizeFilePriority, type FolderPriority } from "@/lib/file-priority"
import { getLinuxFileName, getLinuxFolderName } from "@/lib/incognito"
import type { TorrentFile } from "@/types"

export interface FileTreeNode {
  id: string
  name: string
  kind: "file" | "folder"
  file?: TorrentFile
  children?: FileTreeNode[]
  totalSize: number
  totalProgress: number
  selectedCount: number
  totalCount: number
  priority: FolderPriority
}

export type FileSortColumn = "name" | "size" | "progress" | "priority"

export interface FileSort {
  column: FileSortColumn
  direction: "asc" | "desc"
}

// Name ascending is the order the tree had before headers became clickable.
export const DEFAULT_FILE_SORT: FileSort = { column: "name", direction: "asc" }

export function toggleFileSort(current: FileSort, column: FileSortColumn): FileSort {
  if (current.column !== column) return { column, direction: "asc" }
  return { column, direction: current.direction === "asc" ? "desc" : "asc" }
}

function compareName(a: FileTreeNode, b: FileTreeNode): number {
  return a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: "base" })
}

function sortKey(node: FileTreeNode, column: FileSortColumn): number {
  switch (column) {
    case "size":
      return node.totalSize
    case "progress":
      return node.totalSize > 0 ? node.totalProgress / node.totalSize : 0
    case "priority":
      return node.priority === "mixed" ? FILE_PRIORITY.normal : node.priority
    default:
      return 0
  }
}

// Folders always sort above files. Inside each group the active column applies,
// then name ascending breaks ties.
function compareNodes(a: FileTreeNode, b: FileTreeNode, sort: FileSort): number {
  if (a.kind !== b.kind) return a.kind === "folder" ? -1 : 1
  const sign = sort.direction === "asc" ? 1 : -1
  const primary = sort.column === "name" ? compareName(a, b) : sortKey(a, sort.column) - sortKey(b, sort.column)
  return primary !== 0 ? sign * primary : compareName(a, b)
}

// Returns a new tree with every level reordered; the input nodes are not mutated.
export function sortFileTree(nodes: FileTreeNode[], sort: FileSort): FileTreeNode[] {
  return nodes
    .map(node => (node.children ? { ...node, children: sortFileTree(node.children, sort) } : node))
    .sort((a, b) => compareNodes(a, b, sort))
}

// Builds the folder tree for the Content tab with subtree aggregates on every
// folder node. Nodes come back in file order; callers run sortFileTree.
export function buildFileTree(
  files: TorrentFile[],
  incognitoMode: boolean,
  torrentHash: string
): { nodes: FileTreeNode[]; folderIds: Set<string> } {
  const nodeMap = new Map<string, FileTreeNode>()
  const roots: FileTreeNode[] = []
  const folderIds = new Set<string>()

  for (const file of files) {
    const segments = file.name.split("/").filter(Boolean)
    let parentPath = ""

    for (let i = 0; i < segments.length; i++) {
      const segment = segments[i]
      const currentPath = parentPath ? `${parentPath}/${segment}` : segment
      const isLeaf = i === segments.length - 1

      if (!nodeMap.has(currentPath)) {
        let displayName = segment
        if (incognitoMode) {
          displayName = isLeaf? getLinuxFileName(torrentHash, file.index).split("/").pop() || segment: getLinuxFolderName(torrentHash, i)
        }

        const node: FileTreeNode = {
          id: currentPath,
          name: displayName,
          kind: isLeaf ? "file" : "folder",
          file: isLeaf ? file : undefined,
          children: isLeaf ? undefined : [],
          totalSize: isLeaf ? file.size : 0,
          totalProgress: isLeaf ? file.progress * file.size : 0,
          selectedCount: isLeaf && file.priority !== 0 ? 1 : 0,
          totalCount: isLeaf ? 1 : 0,
          priority: isLeaf ? normalizeFilePriority(file.priority) : FILE_PRIORITY.normal,
        }
        nodeMap.set(currentPath, node)
        if (!isLeaf) folderIds.add(currentPath)

        if (parentPath) {
          nodeMap.get(parentPath)?.children?.push(node)
        } else {
          roots.push(node)
        }
      }

      parentPath = currentPath
    }
  }

  function aggregate(node: FileTreeNode): void {
    if (!node.children) return
    node.children.forEach(aggregate)
    node.totalSize = node.children.reduce((sum, child) => sum + child.totalSize, 0)
    node.totalProgress = node.children.reduce((sum, child) => sum + child.totalProgress, 0)
    node.selectedCount = node.children.reduce((sum, child) => sum + child.selectedCount, 0)
    node.totalCount = node.children.reduce((sum, child) => sum + child.totalCount, 0)
    if (node.children.length > 0) {
      node.priority = node.children.map(child => child.priority).reduce(foldFolderPriority)
    }
  }
  roots.forEach(aggregate)

  return { nodes: roots, folderIds }
}

// discPathOf returns the torrent-relative path a BDInfo scan takes for a Disc
// node, or null when the node is not a Disc. A Disc is a folder with a direct
// BDMV child, or an .iso file; VIDEO_TS is not one. A BDMV folder at the top
// level means the torrent is one bare Disc, which the backend addresses as ".".
// Detection reads node ids (real paths), so incognito display names do not
// affect it.
export function discPathOf(node: FileTreeNode): string | null {
  if (node.kind === "file") return /\.iso$/i.test(node.id) ? node.id : null
  if (node.children?.some(child => child.kind === "folder" && child.id === `${node.id}/BDMV`)) return node.id
  return node.id === "BDMV" ? "." : null
}
