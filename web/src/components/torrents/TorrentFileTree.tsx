/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FilePrioritySelect } from "@/components/torrents/FilePrioritySelect"
import { Checkbox } from "@/components/ui/checkbox"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { SortIcon } from "@/components/ui/sort-icon"
import { useFileRangeSelection } from "@/hooks/useFileRangeSelection"
import { buildFileTree, sortFileTree, toggleFileSort, type FileSort, type FileSortColumn, type FileTreeNode } from "@/lib/file-tree"
import { reconcileExpandedFolders } from "@/lib/file-tree-expansion"
import { getLinuxSavePath } from "@/lib/incognito"
import { cn, copyTextToClipboard, formatBytes, joinPath } from "@/lib/utils"
import type { TorrentFile } from "@/types"
import { useVirtualizer } from "@tanstack/react-virtual"
import { ChevronRight, Copy, Download, FilePen, FolderPen, Info, Loader2 } from "lucide-react"
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface TorrentFileTreeProps {
  files: TorrentFile[]
  sort: FileSort
  supportsFilePriority: boolean
  pendingFileIndices: Set<number>
  incognitoMode: boolean
  torrentHash: string
  savePath?: string
  onToggleFile: (file: TorrentFile, selected: boolean) => void
  onToggleFileRange: (indices: number[], selected: boolean) => void
  onToggleFolder: (folderPath: string, selected: boolean) => void
  onSetFilePriority: (file: TorrentFile, priority: number) => void
  onSetFolderPriority: (folderPath: string, priority: number) => void
  onRenameFile: (filePath: string) => void
  onRenameFolder: (folderPath: string) => void
  onDownloadFile?: (file: TorrentFile) => void
  onShowMediaInfo?: (file: TorrentFile) => void
}

interface FlatRow {
  node: FileTreeNode
  depth: number
  isExpanded: boolean
  hasChildren: boolean
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

const SORT_COLUMNS: { column: FileSortColumn; labelKey: string }[] = [
  { column: "name", labelKey: "fileTable.headers.name" },
  { column: "progress", labelKey: "fileTable.headers.progress" },
  { column: "size", labelKey: "fileTable.headers.size" },
  { column: "priority", labelKey: "filePriority.header" },
]

// Sort control for the vertical layout, which has no header row to click.
// Rendered by the details panel above the ScrollArea that holds the tree.
export function TorrentFileSortBar({
  sort,
  supportsFilePriority,
  onSortChange,
}: {
  sort: FileSort
  supportsFilePriority: boolean
  onSortChange: (sort: FileSort) => void
}) {
  const { t } = useTranslation("torrents")
  return (
    <div className="flex flex-wrap gap-1 px-4 sm:px-6 py-1.5 border-b">
      {SORT_COLUMNS.filter(({ column }) => supportsFilePriority || column !== "priority").map(({ column, labelKey }) => {
        const active = sort.column === column
        return (
          <button
            key={column}
            type="button"
            className={cn(
              "flex-1 h-9 flex items-center justify-center gap-1 rounded-md text-xs font-medium",
              active ? "bg-muted text-foreground" : "text-muted-foreground hover:bg-muted/50"
            )}
            aria-pressed={active}
            aria-label={active ? `${t(labelKey)}, ${t(`sort.${sort.direction === "asc" ? "ascending" : "descending"}`)}` : undefined}
            onClick={() => onSortChange(toggleFileSort(sort, column))}
          >
            <span className="truncate">{t(labelKey)}</span>
            {active && <SortIcon sorted={sort.direction} />}
          </button>
        )
      })}
    </div>
  )
}

export const TorrentFileTree = memo(function TorrentFileTree({
  files,
  sort,
  supportsFilePriority,
  pendingFileIndices,
  incognitoMode,
  torrentHash,
  savePath,
  onToggleFile,
  onToggleFileRange,
  onToggleFolder,
  onSetFilePriority,
  onSetFolderPriority,
  onRenameFile,
  onRenameFolder,
  onDownloadFile,
  onShowMediaInfo,
}: TorrentFileTreeProps) {
  const { t } = useTranslation("torrents")
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  const { nodes: unsortedNodes, folderIds } = useMemo(
    () => buildFileTree(files, incognitoMode, torrentHash),
    [files, incognitoMode, torrentHash]
  )
  const nodes = useMemo(() => sortFileTree(unsortedNodes, sort), [unsortedNodes, sort])

  // Start with all folders expanded
  const [expandedFolders, setExpandedFolders] = useState<Set<string>>(
    () => new Set(folderIds)
  )
  const [knownFolderIds, setKnownFolderIds] = useState(folderIds)

  // Keep expandedFolders in sync when folder paths change (e.g., after rename);
  // render-time adjustment so the reconciled tree commits in one pass
  if (knownFolderIds !== folderIds) {
    setKnownFolderIds(folderIds)
    setExpandedFolders(
      reconcileExpandedFolders(expandedFolders, knownFolderIds, folderIds)
    )
  }

  const flatRows = useMemo(
    () => flattenTree(nodes, expandedFolders),
    [nodes, expandedFolders]
  )

  // Row height: ~48px for tree rows (two lines with padding; the name line carries the
  // compact priority dropdown when file priority is supported)
  const ROW_HEIGHT = 48

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

  const { handleCheckboxPointerDown, handleFileCheckbox } = useFileRangeSelection({
    getRows: () => flatRows,
    onToggleFile,
    onToggleFileRange,
    resetKey: torrentHash,
  })

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
                          onPointerDown={handleCheckboxPointerDown}
                          onCheckedChange={(checked) => handleFileCheckbox(file, virtualRow.index, checked === true)}
                          aria-label={isSkipped ? t("fileTree.selectFileForDownload") : t("fileTree.skipFileDownload")}
                          className="shrink-0"
                        />
                      )}
                      <span className={cn(
                        "flex-1 min-w-0 text-xs font-mono truncate",
                        isSkipped && supportsFilePriority && "text-muted-foreground/70"
                      )}>
                        {node.name}
                      </span>
                      {supportsFilePriority && (
                        <FilePrioritySelect
                          value={node.priority}
                          disabled={isPending}
                          onChange={(priority) => onSetFilePriority(file, priority)}
                          className="w-32 shrink-0"
                        />
                      )}
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
                        aria-label={t("fileTree.renameFile")}
                        title={t("fileTree.renameFile")}
                      >
                        <FilePen className="h-3 w-3" />
                      </button>
                    </div>
                  </div>
                </ContextMenuTrigger>
                <ContextMenuContent>
                  <ContextMenuItem
                    onClick={async () => {
                      const fullPath = incognitoMode? joinPath(getLinuxSavePath(torrentHash), node.name): savePath? joinPath(savePath, file.name): file.name
                      try {
                        await copyTextToClipboard(fullPath)
                        toast.success(t("fileTree.filePathCopied"))
                      } catch {
                        toast.error(t("fileTree.copyPathFailed"))
                      }
                    }}
                  >
                    <Copy className="h-4 w-4 mr-2" />
                    {t("fileTree.copyPath")}
                  </ContextMenuItem>
                  {onDownloadFile && file && (
                    <ContextMenuItem
                      onClick={() => onDownloadFile(file)}
                      disabled={incognitoMode}
                    >
                      <Download className="h-4 w-4 mr-2" />
                      {t("fileTree.download")}
                    </ContextMenuItem>
                  )}
                  {onShowMediaInfo && file && (
                    <ContextMenuItem
                      onClick={() => onShowMediaInfo(file)}
                      disabled={incognitoMode}
                    >
                      <Info className="h-4 w-4 mr-2" />
                      {t("fileTree.mediaInfo")}
                    </ContextMenuItem>
                  )}
                  <ContextMenuItem
                    onClick={() => onRenameFile(file.name)}
                    disabled={incognitoMode}
                  >
                    <FilePen className="h-4 w-4 mr-2" />
                    {t("fileTree.renameFile")}
                  </ContextMenuItem>
                </ContextMenuContent>
              </ContextMenu>
            )
          }

          // Folder row
          const progressPercent = node.totalSize > 0? (node.totalProgress / node.totalSize) * 100: 0
          const isFolderComplete = progressPercent === 100
          const checkState: boolean | "indeterminate" = node.selectedCount === 0? false: node.selectedCount === node.totalCount? true: "indeterminate"
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
                        aria-label={t("fileTree.selectAllFilesIn", { name: node.name })}
                        className="shrink-0"
                      />
                    )}
                    <span className="flex-1 min-w-0 text-xs font-medium truncate">
                      {node.name}/
                    </span>
                    {supportsFilePriority && (
                      <FilePrioritySelect
                        value={node.priority}
                        onChange={(priority) => onSetFolderPriority(node.id, priority)}
                        className="w-32 shrink-0"
                      />
                    )}
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
                      aria-label={t("fileTree.renameFolder")}
                      title={t("fileTree.renameFolder")}
                    >
                      <FolderPen className="h-3 w-3" />
                    </button>
                  </div>
                </div>
              </ContextMenuTrigger>
              <ContextMenuContent>
                <ContextMenuItem
                  onClick={async (e) => {
                    e.stopPropagation()
                    const fullPath = incognitoMode? joinPath(getLinuxSavePath(torrentHash), node.name): savePath? joinPath(savePath, node.id): node.id
                    try {
                      await copyTextToClipboard(fullPath)
                      toast.success(t("fileTree.folderPathCopied"))
                    } catch {
                      toast.error(t("fileTree.copyPathFailed"))
                    }
                  }}
                >
                  <Copy className="h-4 w-4 mr-2" />
                  {t("fileTree.copyPath")}
                </ContextMenuItem>
                <ContextMenuItem
                  onClick={(e) => {
                    e.stopPropagation()
                    onRenameFolder(node.id)
                  }}
                  disabled={incognitoMode}
                >
                  <FolderPen className="h-4 w-4 mr-2" />
                  {t("fileTree.renameFolder")}
                </ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>
          )
        })}
      </div>
    </div>
  )
})
