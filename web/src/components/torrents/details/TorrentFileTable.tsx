/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Checkbox } from "@/components/ui/checkbox"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { ScrollArea } from "@/components/ui/scroll-area"
import { TruncatedText } from "@/components/ui/truncated-text"
import { getLinuxFileName, getLinuxFolderName } from "@/lib/incognito"
import { cn, formatBytes } from "@/lib/utils"
import type { TorrentFile } from "@/types"
import { ChevronDown, ChevronRight, File, Folder, Loader2, Pencil, Search, X } from "lucide-react"
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
  depth: number
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
          depth: i,
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
}: TorrentFileTableProps) {
  const [expandedFolders, setExpandedFolders] = useState<Set<string>>(() => new Set())
  const [searchQuery, setSearchQuery] = useState("")
  const initializedForHash = useRef<string | null>(null)

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

      <ScrollArea className="flex-1 min-h-0">
        <div className="min-w-[500px]">
          <table className="w-full text-xs table-fixed">
            <thead className="sticky top-0 z-10 bg-background border-b">
              <tr>
                {supportsFilePriority && (
                  <th className="w-8 px-2 py-1.5 text-left"></th>
                )}
                <th className="px-2 py-1.5 text-left font-medium text-muted-foreground">Name</th>
                <th className="w-28 px-2 py-1.5 text-left font-medium text-muted-foreground">Progress</th>
                <th className="w-24 px-2 py-1.5 text-right font-medium text-muted-foreground">Size</th>
              </tr>
            </thead>
            <tbody>
              {filteredRows.map((row) => {
                const { node, depth, isExpanded, hasChildren } = row
                const isFile = node.kind === "file"
                const file = node.file
                const isPending = file && pendingFileIndices.has(file.index)
                const isSelected = isFile ? (file?.priority !== 0) : (node.selectedCount === node.totalCount)
                const isIndeterminate = !isFile && node.selectedCount > 0 && node.selectedCount < node.totalCount
                const progress = node.totalSize > 0 ? (node.totalProgress / node.totalSize) * 100 : 0

                const rowContent = (
                  <tr
                    key={node.id}
                    className="border-b border-border/30 hover:bg-muted/30 cursor-default"
                  >
                    {supportsFilePriority && (
                      <td className="px-2 py-1.5">
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
                      </td>
                    )}
                    <td className="px-2 py-1.5 overflow-hidden">
                      <div
                        className="flex items-center gap-1 min-w-0"
                        style={{ paddingLeft: depth * 16 }}
                      >
                        {hasChildren ? (
                          <button
                            className="p-0.5 hover:bg-muted rounded"
                            onClick={() => toggleFolder(node.id)}
                          >
                            {isExpanded ? (
                              <ChevronDown className="h-3.5 w-3.5" />
                            ) : (
                              <ChevronRight className="h-3.5 w-3.5" />
                            )}
                          </button>
                        ) : (
                          <span className="w-4" />
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
                          <span className="text-muted-foreground ml-1">
                            ({node.totalCount})
                          </span>
                        )}
                      </div>
                    </td>
                    <td className="px-2 py-1.5">
                      <div className="flex items-center gap-2">
                        <Progress value={progress} className="h-1.5 w-16" />
                        <span className="tabular-nums text-[10px] text-muted-foreground w-10">
                          {progress.toFixed(1)}%
                        </span>
                      </div>
                    </td>
                    <td className="px-2 py-1.5 text-right tabular-nums">
                      {formatBytes(node.totalSize)}
                    </td>
                  </tr>
                )

                // Wrap with context menu if rename handlers are provided
                if (onRenameFile || onRenameFolder) {
                  return (
                    <ContextMenu key={node.id}>
                      <ContextMenuTrigger asChild>
                        {rowContent}
                      </ContextMenuTrigger>
                      <ContextMenuContent>
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

                return rowContent
              })}
            </tbody>
          </table>
        </div>
      </ScrollArea>
    </div>
  )
})
