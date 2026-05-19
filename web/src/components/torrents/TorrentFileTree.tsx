/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Checkbox } from "@/components/ui/checkbox"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { getLinuxFileName } from "@/lib/incognito"
import { cn, formatBytes } from "@/lib/utils"
import type { TorrentFile } from "@/types"
import { useVirtualizer } from "@tanstack/react-virtual"
import { ChevronRight, Download, FilePen, FolderPen, Info, Loader2 } from "lucide-react"
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"

interface TorrentFileTreeProps {
  files: TorrentFile[]
  supportsFilePriority: boolean
  pendingFileIndices: Set<number>
  incognitoMode: boolean
  torrentHash: string
  onToggleFile: (file: TorrentFile, selected: boolean) => void
  onToggleFolder: (folderPath: string, selected: boolean) => void
  onRenameFile: (filePath: string) => void
  onRenameFolder: (folderPath: string) => void
  onDownloadFile?: (file: TorrentFile) => void
  onShowMediaInfo?: (file: TorrentFile) => void
}

interface FileTreeNode {
  id: string
  name: string
  kind: "file" | "folder"
  file?: TorrentFile
  children?: FileTreeNode[]
  totalSize: number
  totalProgress: number
  selectedCount: number
  totalCount: number
}

interface FlatRow {
  node: FileTreeNode
  depth: number
  isExpanded: boolean
  hasChildren: boolean
}

function buildFileTree(
  files: TorrentFile[],
  incognitoMode: boolean,
  torrentHash: string
): { nodes: FileTreeNode[]; allFolderIds: string[] } {
  const nodeMap = new Map<string, FileTreeNode>()
  const roots: FileTreeNode[] = []
  const allFolderIds: string[] = []

  // Sort files by name for consistent ordering
  const sortedFiles = [...files].sort((a, b) => a.name.localeCompare(b.name))

  for (const file of sortedFiles) {
    const segments = file.name.split("/").filter(Boolean)
    let parentPath = ""

    for (let i = 0; i < segments.length; i++) {
      const segment = segments[i]
      const currentPath = parentPath ? `${parentPath}/${segment}` : segment
      const isLeaf = i === segments.length - 1

      let node = nodeMap.get(currentPath)

      if (!node) {
        const displayName = incognitoMode && isLeaf
          ? getLinuxFileName(torrentHash, file.index).split("/").pop() || segment
          : segment

        node = {
          id: currentPath,
          name: displayName,
          kind: isLeaf ? "file" : "folder",
          file: isLeaf ? file : undefined,
          children: isLeaf ? undefined : [],
          totalSize: isLeaf ? file.size : 0,
          totalProgress: isLeaf ? file.progress * file.size : 0,
          selectedCount: isLeaf && file.priority !== 0 ? 1 : 0,
          totalCount: isLeaf ? 1 : 0,
        }
        nodeMap.set(currentPath, node)

        if (!isLeaf) {
          allFolderIds.push(currentPath)
        }

        if (parentPath) {
          const parentNode = nodeMap.get(parentPath)
          if (parentNode && parentNode.children) {
            parentNode.children.push(node)
          }
        } else {
          roots.push(node)
        }
      }

      parentPath = currentPath
    }
  }

  // Calculate aggregates bottom-up
  function calculateAggregates(node: FileTreeNode): void {
    if (node.kind === "folder" && node.children) {
      let totalSize = 0
      let totalProgress = 0
      let selectedCount = 0
      let totalCount = 0

      for (const child of node.children) {
        calculateAggregates(child)
        totalSize += child.totalSize
        totalProgress += child.totalProgress
        selectedCount += child.selectedCount
        totalCount += child.totalCount
      }

      node.totalSize = totalSize
      node.totalProgress = totalProgress
      node.selectedCount = selectedCount
      node.totalCount = totalCount

      // Sort children: folders first, then files, both alphabetically
      node.children.sort((a, b) => {
        if (a.kind !== b.kind) {
          return a.kind === "folder" ? -1 : 1
        }
        return a.name.localeCompare(b.name)
      })
    }
  }

  for (const root of roots) {
    calculateAggregates(root)
  }

  // Sort roots: folders first, then files
  roots.sort((a, b) => {
    if (a.kind !== b.kind) {
      return a.kind === "folder" ? -1 : 1
    }
    return a.name.localeCompare(b.name)
  })

  return { nodes: roots, allFolderIds }
}

function flattenTree(
  nodes: FileTreeNode[],
  expandedFolders: Set<string>,
  depth = 0
): FlatRow[] {
  const rows: FlatRow[] = []

  for (const node of nodes) {
    const hasChildren = node.kind === "folder" && Boolean(node.children?.length)
    const isExpanded = expandedFolders.has(node.id)

    rows.push({ node, depth, isExpanded, hasChildren })

    if (hasChildren && isExpanded && node.children) {
      rows.push(...flattenTree(node.children, expandedFolders, depth + 1))
    }
  }

  return rows
}

export const TorrentFileTree = memo(function TorrentFileTree({
  files,
  supportsFilePriority,
  pendingFileIndices,
  incognitoMode,
  torrentHash,
  onToggleFile,
  onToggleFolder,
  onRenameFile,
  onRenameFolder,
  onDownloadFile,
  onShowMediaInfo,
}: TorrentFileTreeProps) {
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  const { nodes, allFolderIds } = useMemo(
    () => buildFileTree(files, incognitoMode, torrentHash),
    [files, incognitoMode, torrentHash]
  )

  // Start with all folders expanded
  const [expandedFolders, setExpandedFolders] = useState<Set<string>>(
    () => new Set(allFolderIds)
  )

  // Keep expandedFolders in sync when folder paths change (e.g., after rename)
  useEffect(() => {
    setExpandedFolders((prev) => {
      const allFolderSet = new Set(allFolderIds)
      const next = new Set(prev)
      let changed = false

      // Remove folders that no longer exist
      for (const id of prev) {
        if (!allFolderSet.has(id)) {
          next.delete(id)
          changed = true
        }
      }

      // Add new folders as expanded by default
      for (const id of allFolderIds) {
        if (!prev.has(id)) {
          next.add(id)
          changed = true
        }
      }

      return changed ? next : prev
    })
  }, [allFolderIds])

  const flatRows = useMemo(
    () => flattenTree(nodes, expandedFolders),
    [nodes, expandedFolders]
  )

  // Row height: ~44px for tree rows (two lines with padding)
  const ROW_HEIGHT = 44

  const virtualizer = useVirtualizer({
    count: flatRows.length,
    getScrollElement: () => scrollContainerRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: flatRows.length > 5000 ? 5 : flatRows.length > 1000 ? 10 : 15,
    getItemKey: useCallback((index: number) => {
      const row = flatRows[index]
      return row ? row.node.id : `row-${index}`
    }, [flatRows]),
  })

  // Force virtualizer to recalculate when rows change
  useEffect(() => {
    virtualizer.measure()
  }, [flatRows.length, virtualizer])

  const virtualRows = virtualizer.getVirtualItems()

  const toggleFolder = useCallback((folderId: string) => {
    setExpandedFolders((prev) => {
      const next = new Set(prev)
      if (next.has(folderId)) {
        next.delete(folderId)
      } else {
        next.add(folderId)
      }
      return next
    })
  }, [])

  return (
    <div
      ref={scrollContainerRef}
      className="w-full min-w-0 h-full overflow-auto scrollbar-thin"
      onContextMenu={(e) => e.preventDefault()}
    >
      <div
        style={{
          height: `${virtualizer.getTotalSize()}px`,
          width: "100%",
          position: "relative",
        }}
      >
        {virtualRows.map((virtualRow) => {
          const row = flatRows[virtualRow.index]
          if (!row) return null

          const { node, depth, isExpanded, hasChildren } = row
          const isFile = node.kind === "file"
          const file = node.file
          const isPending = file && pendingFileIndices.has(file.index)

          if (isFile && file) {
            // File row
            const isSkipped = file.priority === 0
            const isComplete = file.progress === 1
            const progressPercent = file.progress * 100
            const indent = depth * 20 + 28

            return (
              <ContextMenu key={node.id} modal={false}>
                <ContextMenuTrigger asChild>
                  <div
                    className={cn(
                      "flex flex-col gap-0.5 py-1 pr-2 rounded-md transition-colors cursor-default",
                      "hover:bg-muted/50",
                      isSkipped && "opacity-60"
                    )}
                    style={{
                      position: "absolute",
                      top: 0,
                      left: 0,
                      width: "100%",
                      height: `${virtualRow.size}px`,
                      transform: `translateY(${virtualRow.start}px)`,
                      paddingLeft: `${indent}px`,
                    }}
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      {supportsFilePriority && (
                        <Checkbox
                          checked={!isSkipped}
                          disabled={isPending}
                          onCheckedChange={(checked) => onToggleFile(file, checked === true)}
                          aria-label={isSkipped ? "Select file for download" : "Skip file download"}
                          className="shrink-0"
                        />
                      )}
                      <span className={cn(
                        "text-xs font-mono truncate",
                        isSkipped && supportsFilePriority && "text-muted-foreground/70"
                      )}>
                        {node.name}
                      </span>
                    </div>
                    <div className="flex items-center gap-2" style={{ paddingLeft: supportsFilePriority ? "24px" : "0" }}>
                      {isPending && (
                        <Loader2 className="h-3 w-3 animate-spin text-muted-foreground shrink-0" />
                      )}
                      <span className="text-[10px] text-muted-foreground tabular-nums whitespace-nowrap">
                        <span className={isComplete ? "text-green-500" : ""}>{Math.round(progressPercent)}%</span>
                        <span className="mx-1">·</span>
                        {formatBytes(file.size)}
                      </span>
                      <button
                        type="button"
                        className={cn(
                          "p-0.5 rounded text-muted-foreground transition-colors",
                          incognitoMode ? "opacity-50 cursor-not-allowed" : "hover:bg-muted/80 hover:text-foreground"
                        )}
                        onClick={(e) => {
                          e.stopPropagation()
                          if (!incognitoMode) onRenameFile(file.name)
                        }}
                        disabled={incognitoMode}
                        aria-label="Rename file"
                        title="Rename file"
                      >
                        <FilePen className="h-3 w-3" />
                      </button>
                    </div>
                  </div>
                </ContextMenuTrigger>
                <ContextMenuContent>
                  {onDownloadFile && file && (
                    <ContextMenuItem
                      onClick={() => onDownloadFile(file)}
                      disabled={incognitoMode}
                    >
                      <Download className="h-4 w-4 mr-2" />
                      Download
                    </ContextMenuItem>
                  )}
                  {onShowMediaInfo && file && (
                    <ContextMenuItem
                      onClick={() => onShowMediaInfo(file)}
                      disabled={incognitoMode}
                    >
                      <Info className="h-4 w-4 mr-2" />
                      MediaInfo
                    </ContextMenuItem>
                  )}
                  <ContextMenuItem
                    onClick={() => onRenameFile(file.name)}
                    disabled={incognitoMode}
                  >
                    <FilePen className="h-4 w-4 mr-2" />
                    Rename
                  </ContextMenuItem>
                </ContextMenuContent>
              </ContextMenu>
            )
          }

          // Folder row
          const progressPercent = node.totalSize > 0
            ? (node.totalProgress / node.totalSize) * 100
            : 0
          const isFolderComplete = progressPercent === 100
          const checkState: boolean | "indeterminate" = node.selectedCount === 0
            ? false
            : node.selectedCount === node.totalCount
              ? true
              : "indeterminate"
          const indent = depth * 20 + 4

          const handleCheckChange = () => {
            const shouldSelect = checkState !== true
            onToggleFolder(node.id, shouldSelect)
          }

          return (
            <ContextMenu key={node.id} modal={false}>
              <ContextMenuTrigger asChild>
                <div
                  className={cn(
                    "flex flex-col gap-0.5 py-1 pr-2 rounded-md transition-colors cursor-pointer",
                    "hover:bg-muted/50"
                  )}
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    height: `${virtualRow.size}px`,
                    transform: `translateY(${virtualRow.start}px)`,
                    paddingLeft: `${indent}px`,
                  }}
                  onClick={() => hasChildren && toggleFolder(node.id)}
                >
                  <div className="flex items-center gap-2 min-w-0">
                    <ChevronRight
                      className={cn(
                        "h-4 w-4 shrink-0 transition-transform duration-200",
                        isExpanded && "rotate-90"
                      )}
                    />
                    {supportsFilePriority && (
                      <Checkbox
                        checked={checkState}
                        onCheckedChange={handleCheckChange}
                        onClick={(e) => e.stopPropagation()}
                        aria-label={`Select all files in ${node.name}`}
                        className="shrink-0"
                      />
                    )}
                    <span className="text-xs font-medium truncate">
                      {node.name}/
                    </span>
                  </div>
                  <div className="flex items-center gap-2" style={{ paddingLeft: supportsFilePriority ? "40px" : "24px" }}>
                    <span className="text-[10px] text-muted-foreground tabular-nums whitespace-nowrap">
                      <span className={isFolderComplete ? "text-green-500" : ""}>{Math.round(progressPercent)}%</span>
                      <span className="mx-1">·</span>
                      {formatBytes(node.totalSize)}
                    </span>
                    <button
                      type="button"
                      className={cn(
                        "p-0.5 rounded text-muted-foreground transition-colors",
                        incognitoMode ? "opacity-50 cursor-not-allowed" : "hover:bg-muted/80 hover:text-foreground"
                      )}
                      onClick={(e) => {
                        e.stopPropagation()
                        if (!incognitoMode) onRenameFolder(node.id)
                      }}
                      disabled={incognitoMode}
                      aria-label="Rename folder"
                      title="Rename folder"
                    >
                      <FolderPen className="h-3 w-3" />
                    </button>
                  </div>
                </div>
              </ContextMenuTrigger>
              <ContextMenuContent>
                <ContextMenuItem
                  onClick={(e) => {
                    e.stopPropagation()
                    onRenameFolder(node.id)
                  }}
                  disabled={incognitoMode}
                >
                  <FolderPen className="h-4 w-4 mr-2" />
                  Rename
                </ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>
          )
        })}
      </div>
    </div>
  )
})
