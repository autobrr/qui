/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Checkbox } from "@/components/ui/checkbox"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { TruncatedText } from "@/components/ui/truncated-text"
import { getLinuxFileName, getLinuxFolderName } from "@/lib/incognito"
import { cn, formatBytes } from "@/lib/utils"
import type { TorrentFile } from "@/types"
import { useVirtualizer } from "@tanstack/react-virtual"
import { ChevronDown, ChevronRight, Download, File, Folder, Loader2, Pencil, Search, X } from "lucide-react"
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"

interface TorrentFileTableProps {
  files: TorrentFile[] | undefined
  loading: boolean
  supportsFilePriority: boolean
  pendingFileIndices: Set<number>
  incognitoMode: boolean
  torrentHash: string
  onToggleFile: (file: TorrentFile, selected: boolean) => void
  onToggleFolder: (folderPath: string, selected: boolean) => void
  onRenameFile?: (filePath: string) => void
  onRenameFolder?: (folderPath: string) => void
  onDownloadFile?: (file: TorrentFile) => void
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
): FileTreeNode[] {
  const nodeMap = new Map<string, FileTreeNode>()
  const roots: FileTreeNode[] = []

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
        let displayName: string
        if (incognitoMode) {
          if (isLeaf) {
            displayName = getLinuxFileName(torrentHash, file.index).split("/").pop() || segment
          } else {
            displayName = getLinuxFolderName(torrentHash, i)
          }
        } else {
          displayName = segment
        }

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
      node.children.forEach(calculateAggregates)
      node.totalSize = node.children.reduce((sum, child) => sum + child.totalSize, 0)
      node.totalProgress = node.children.reduce((sum, child) => sum + child.totalProgress, 0)
      node.selectedCount = node.children.reduce((sum, child) => sum + child.selectedCount, 0)
      node.totalCount = node.children.reduce((sum, child) => sum + child.totalCount, 0)
    }
  }

  roots.forEach(calculateAggregates)

  // Sort nodes: folders first, then alphabetically within each type (natural sort)
  function sortNodes(nodes: FileTreeNode[]): void {
    nodes.sort((a, b) => {
      // Folders before files
      if (a.kind === "folder" && b.kind === "file") return -1
      if (a.kind === "file" && b.kind === "folder") return 1
      // Alphabetical within same type (natural sort)
      return a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: "base" })
    })
    for (const node of nodes) {
      if (node.children) sortNodes(node.children)
    }
  }
  sortNodes(roots)

  return roots
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

export const TorrentFileTable = memo(function TorrentFileTable({
  files,
  loading,
  supportsFilePriority,
  pendingFileIndices,
  incognitoMode,
  torrentHash,
  onToggleFile,
  onToggleFolder,
  onRenameFile,
  onRenameFolder,
  onDownloadFile,
}: TorrentFileTableProps) {
  const [expandedFolders, setExpandedFolders] = useState<Set<string>>(() => new Set())
  const [searchQuery, setSearchQuery] = useState("")
  const initializedForHash = useRef<string | null>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  const tree = useMemo(
    () => (files ? buildFileTree(files, incognitoMode, torrentHash) : []),
    [files, incognitoMode, torrentHash]
  )

  // Expand all folders by default when tree is first built for a new torrent
  useEffect(() => {
    if (tree.length > 0 && initializedForHash.current !== torrentHash) {
      initializedForHash.current = torrentHash
      const allFolderIds = new Set<string>()
      function collectFolders(nodes: FileTreeNode[]) {
        for (const node of nodes) {
          if (node.kind === "folder") {
            allFolderIds.add(node.id)
            if (node.children) collectFolders(node.children)
          }
        }
      }
      collectFolders(tree)
      setExpandedFolders(allFolderIds)
    }
  }, [tree, torrentHash])

  const flatRows = useMemo(
    () => flattenTree(tree, expandedFolders),
    [tree, expandedFolders]
  )

  // Filter rows based on search query
  const filteredRows = useMemo(() => {
    if (!searchQuery.trim()) return flatRows

    const query = searchQuery.toLowerCase()
    const matchingIds = new Set<string>()

    // Find all matching nodes and their parent paths
    for (const row of flatRows) {
      if (row.node.name.toLowerCase().includes(query)) {
        matchingIds.add(row.node.id)
        // Add all parent folders
        const parts = row.node.id.split("/")
        let parentPath = ""
        for (let i = 0; i < parts.length - 1; i++) {
          parentPath = parentPath ? `${parentPath}/${parts[i]}` : parts[i]
          matchingIds.add(parentPath)
        }
      }
    }

    return flatRows.filter(row => matchingIds.has(row.node.id))
  }, [flatRows, searchQuery])

  // Row height: 28px for file rows (with some padding)
  const ROW_HEIGHT = 28

  const virtualizer = useVirtualizer({
    count: filteredRows.length,
    getScrollElement: () => scrollContainerRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: filteredRows.length > 5000 ? 5 : filteredRows.length > 1000 ? 10 : 15,
    getItemKey: useCallback((index: number) => {
      const row = filteredRows[index]
      return row ? row.node.id : `row-${index}`
    }, [filteredRows]),
  })

  // Force virtualizer to recalculate when rows change
  useEffect(() => {
    virtualizer.measure()
  }, [filteredRows.length, virtualizer])

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

  const expandAll = useCallback(() => {
    const allFolderIds = new Set<string>()
    function collectFolders(nodes: FileTreeNode[]) {
      for (const node of nodes) {
        if (node.kind === "folder") {
          allFolderIds.add(node.id)
          if (node.children) collectFolders(node.children)
        }
      }
    }
    collectFolders(tree)
    setExpandedFolders(allFolderIds)
  }, [tree])

  const collapseAll = useCallback(() => {
    setExpandedFolders(new Set())
  }, [])

  if (loading && !files) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="h-5 w-5 animate-spin" />
      </div>
    )
  }

  if (!files || files.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
        No files
      </div>
    )
  }

  return (
    <div className="h-full flex flex-col min-h-0">
      {/* Toolbar */}
      <div className="flex items-center gap-2 px-3 py-1.5 border-b text-xs">
        <button
          className="text-muted-foreground hover:text-foreground"
          onClick={expandAll}
        >
          Expand All
        </button>
        <span className="text-muted-foreground">/</span>
        <button
          className="text-muted-foreground hover:text-foreground"
          onClick={collapseAll}
        >
          Collapse All
        </button>
        <div className="relative ml-2">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Search files..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="h-6 w-40 pl-7 pr-7 text-xs"
          />
          {searchQuery && (
            <button
              onClick={() => setSearchQuery("")}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </div>
        <span className="ml-auto text-muted-foreground">
          {searchQuery ? `${filteredRows.length} of ${files.length}` : `${files.length} file${files.length !== 1 ? "s" : ""}`}
        </span>
      </div>

      <div
        ref={scrollContainerRef}
        className="flex-1 min-h-0 overflow-auto scrollbar-thin"
      >
        <div className="min-w-[500px]">
          {/* Header - sticky */}
          <div className="sticky top-0 z-10 bg-background border-b flex text-xs">
            {supportsFilePriority && (
              <div className="w-8 px-2 py-1.5 text-left shrink-0"></div>
            )}
            <div className="flex-1 px-2 py-1.5 text-left font-medium text-muted-foreground">Name</div>
            <div className="w-28 px-2 py-1.5 text-left font-medium text-muted-foreground shrink-0">Progress</div>
            <div className="w-24 px-2 py-1.5 text-right font-medium text-muted-foreground shrink-0">Size</div>
          </div>
          {/* Virtualized body */}
          <div
            style={{
              height: `${virtualizer.getTotalSize()}px`,
              width: "100%",
              position: "relative",
            }}
          >
            {virtualRows.map((virtualRow) => {
              const row = filteredRows[virtualRow.index]
              if (!row) return null
              const { node, depth, isExpanded, hasChildren } = row
              const isFile = node.kind === "file"
              const file = node.file
              const isPending = file && pendingFileIndices.has(file.index)
              const isSelected = isFile ? (file?.priority !== 0) : (node.selectedCount === node.totalCount)
              const isIndeterminate = !isFile && node.selectedCount > 0 && node.selectedCount < node.totalCount
              const progress = node.totalSize > 0 ? (node.totalProgress / node.totalSize) * 100 : 0

              const rowContent = (
                <div
                  className="flex items-center border-b border-border/30 hover:bg-muted/30 cursor-default text-xs"
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    height: `${virtualRow.size}px`,
                    transform: `translateY(${virtualRow.start}px)`,
                  }}
                >
                  {supportsFilePriority && (
                    <div className="w-8 px-2 py-1.5 shrink-0 flex items-center">
                      <Checkbox
                        checked={isIndeterminate ? "indeterminate" : isSelected}
                        onCheckedChange={(checked) => {
                          if (isFile && file) {
                            onToggleFile(file, checked === true)
                          } else {
                            onToggleFolder(node.id, checked === true)
                          }
                        }}
                        disabled={isPending}
                        className="h-3.5 w-3.5"
                      />
                    </div>
                  )}
                  <div className="flex-1 px-2 py-1.5 overflow-hidden min-w-0">
                    <div
                      className="flex items-center gap-1 min-w-0"
                      style={{ paddingLeft: depth * 16 }}
                    >
                      {hasChildren ? (
                        <button
                          className="p-0.5 hover:bg-muted rounded shrink-0"
                          onClick={() => toggleFolder(node.id)}
                        >
                          {isExpanded ? (
                            <ChevronDown className="h-3.5 w-3.5" />
                          ) : (
                            <ChevronRight className="h-3.5 w-3.5" />
                          )}
                        </button>
                      ) : (
                        <span className="w-4 shrink-0" />
                      )}
                      {isFile ? (
                        <File className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                      ) : (
                        <Folder className="h-3.5 w-3.5 text-yellow-500 shrink-0" />
                      )}
                      <TruncatedText
                        className={cn(isPending && "opacity-50")}
                        tooltipSide="top"
                      >
                        {node.name}
                      </TruncatedText>
                      {!isFile && (
                        <span className="text-muted-foreground ml-1 shrink-0">
                          ({node.totalCount})
                        </span>
                      )}
                    </div>
                  </div>
                  <div className="w-28 px-2 py-1.5 shrink-0">
                    <div className="flex items-center gap-2">
                      <Progress value={progress} className="h-1.5 w-16" />
                      <span className="tabular-nums text-[10px] text-muted-foreground w-10">
                        {(Math.floor(progress * 10) / 10).toFixed(1)}%
                      </span>
                    </div>
                  </div>
                  <div className="w-24 px-2 py-1.5 text-right tabular-nums shrink-0">
                    {formatBytes(node.totalSize)}
                  </div>
                </div>
              )

              // Wrap with context menu if any file action handlers are provided
              if (onRenameFile || onRenameFolder || onDownloadFile) {
                return (
                  <ContextMenu key={node.id}>
                    <ContextMenuTrigger asChild>
                      {rowContent}
                    </ContextMenuTrigger>
                    <ContextMenuContent>
                      {isFile && onDownloadFile && node.file && (
                        <ContextMenuItem
                          onClick={() => onDownloadFile(node.file!)}
                          disabled={incognitoMode}
                        >
                          <Download className="h-3.5 w-3.5 mr-2" />
                          Download
                        </ContextMenuItem>
                      )}
                      {isFile && onRenameFile && (
                        <ContextMenuItem onClick={() => onRenameFile(node.id)}>
                          <Pencil className="h-3.5 w-3.5 mr-2" />
                          Rename File
                        </ContextMenuItem>
                      )}
                      {!isFile && onRenameFolder && (
                        <ContextMenuItem onClick={() => onRenameFolder(node.id)}>
                          <Pencil className="h-3.5 w-3.5 mr-2" />
                          Rename Folder
                        </ContextMenuItem>
                      )}
                    </ContextMenuContent>
                  </ContextMenu>
                )
              }

              return <div key={node.id} className="contents">{rowContent}</div>
            })}
          </div>
        </div>
      </div>
    </div>
  )
})
