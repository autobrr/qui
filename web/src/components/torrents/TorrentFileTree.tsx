/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Checkbox } from "@/components/ui/checkbox"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { getLinuxFileName } from "@/lib/incognito"
import { cn, copyTextToClipboard, formatBytes, joinPath } from "@/lib/utils"
import type { TorrentFile } from "@/types"
import * as AccordionPrimitive from "@radix-ui/react-accordion"
import { ChevronRight, Copy, FilePen, FolderPen, Loader2 } from "lucide-react"
import { memo, useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

interface TorrentFileTreeProps {
  files: TorrentFile[]
  supportsFilePriority: boolean
  pendingFileIndices: Set<number>
  incognitoMode: boolean
  torrentHash: string
  savePath?: string
  onToggleFile: (file: TorrentFile, selected: boolean) => void
  onToggleFolder: (folderPath: string, selected: boolean) => void
  onRenameFile: (filePath: string) => void
  onRenameFolder: (folderPath: string) => void
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

interface FileRowProps {
  node: FileTreeNode
  depth: number
  supportsFilePriority: boolean
  isPending: boolean
  incognitoMode: boolean
  savePath?: string
  onToggle: (file: TorrentFile, selected: boolean) => void
  onRename: (filePath: string) => void
}

const FileRow = memo(function FileRow({
  node,
  depth,
  supportsFilePriority,
  isPending,
  incognitoMode,
  savePath,
  onToggle,
  onRename,
}: FileRowProps) {
  const file = node.file!
  const isSkipped = file.priority === 0
  const isComplete = file.progress === 1
  const progressPercent = file.progress * 100

  // Files need extra indent to align with folder content (chevron width + gap)
  const indent = depth * 20 + 28

  return (
    <ContextMenu modal={false}>
      <ContextMenuTrigger asChild>
        <div
          className={cn(
            "flex flex-col gap-0.5 py-1 pr-2 rounded-md transition-colors cursor-default",
            "hover:bg-muted/50",
            isSkipped && "opacity-60"
          )}
          style={{ paddingLeft: `${indent}px` }}
        >
          <div className="flex items-center gap-2 min-w-0">
            {supportsFilePriority && (
              <Checkbox
                checked={!isSkipped}
                disabled={isPending}
                onCheckedChange={(checked) => onToggle(file, checked === true)}
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
                if (!incognitoMode) onRename(file.name)
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
        <ContextMenuItem
          onClick={async () => {
            const fullPath = savePath ? joinPath(savePath, file.name) : file.name
            try {
              await copyTextToClipboard(fullPath)
            } catch {
              toast.error("Failed to copy path to clipboard")
            }
          }}
        >
          <Copy className="h-4 w-4 mr-2" />
          Copy Path
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onRename(file.name)}
          disabled={incognitoMode}
        >
          <FilePen className="h-4 w-4 mr-2" />
          Rename File
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
})

interface FolderRowProps {
  node: FileTreeNode
  depth: number
  isExpanded: boolean
  supportsFilePriority: boolean
  incognitoMode: boolean
  savePath?: string
  onToggle: (folderPath: string, selected: boolean) => void
  onRename: (folderPath: string) => void
}

const FolderRow = memo(function FolderRow({
  node,
  depth,
  isExpanded,
  supportsFilePriority,
  incognitoMode,
  savePath,
  onToggle,
  onRename,
}: FolderRowProps) {
  const progressPercent = node.totalSize > 0
    ? (node.totalProgress / node.totalSize) * 100
    : 0
  const isComplete = progressPercent === 100

  // Determine checkbox state
  const checkState: boolean | "indeterminate" = node.selectedCount === 0
    ? false
    : node.selectedCount === node.totalCount
      ? true
      : "indeterminate"

  const handleCheckChange = useCallback(() => {
    // If none or some selected, select all. If all selected, deselect all.
    const shouldSelect = checkState !== true
    onToggle(node.id, shouldSelect)
  }, [checkState, node.id, onToggle])

  const indent = depth * 20 + 4

  return (
    <ContextMenu modal={false}>
      <ContextMenuTrigger asChild>
        <div
          className={cn(
            "flex flex-col gap-0.5 py-1 pr-2 rounded-md transition-colors cursor-pointer",
            "hover:bg-muted/50"
          )}
          style={{ paddingLeft: `${indent}px` }}
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
              <span className={isComplete ? "text-green-500" : ""}>{Math.round(progressPercent)}%</span>
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
                if (!incognitoMode) onRename(node.id)
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
          onClick={async (e) => {
            e.stopPropagation()
            const fullPath = savePath ? joinPath(savePath, node.id) : node.id
            try {
              await copyTextToClipboard(fullPath)
            } catch {
              toast.error("Failed to copy path to clipboard")
            }
          }}
        >
          <Copy className="h-4 w-4 mr-2" />
          Copy Path
        </ContextMenuItem>
        <ContextMenuItem
          onClick={(e) => {
            e.stopPropagation()
            onRename(node.id)
          }}
          disabled={incognitoMode}
        >
          <FolderPen className="h-4 w-4 mr-2" />
          Rename Folder
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
})

interface TreeNodeProps {
  node: FileTreeNode
  depth: number
  supportsFilePriority: boolean
  pendingFileIndices: Set<number>
  incognitoMode: boolean
  savePath?: string
  folderState: Map<string, boolean>
  onToggleFile: (file: TorrentFile, selected: boolean) => void
  onToggleFolder: (folderPath: string, selected: boolean) => void
  onRenameFile: (filePath: string) => void
  onRenameFolder: (folderPath: string) => void
}

const TreeNode = memo(function TreeNode({
  node,
  depth,
  supportsFilePriority,
  pendingFileIndices,
  incognitoMode,
  savePath,
  folderState,
  onToggleFile,
  onToggleFolder,
  onRenameFile,
  onRenameFolder,
}: TreeNodeProps) {
  if (node.kind === "file" && node.file) {
    return (
      <FileRow
        node={node}
        depth={depth}
        supportsFilePriority={supportsFilePriority}
        isPending={pendingFileIndices.has(node.file.index)}
        incognitoMode={incognitoMode}
        savePath={savePath}
        onToggle={onToggleFile}
        onRename={onRenameFile}
      />
    )
  }

  const isExpanded = folderState.get(node.id) === true

  return (
    <AccordionPrimitive.Item value={node.id} className="border-none w-full min-w-0">
      <AccordionPrimitive.Header className="w-full min-w-0">
        <AccordionPrimitive.Trigger asChild className="w-full min-w-0">
          <div className="w-full min-w-0">
            <FolderRow
              node={node}
              depth={depth}
              isExpanded={isExpanded}
              supportsFilePriority={supportsFilePriority}
              incognitoMode={incognitoMode}
              savePath={savePath}
              onToggle={onToggleFolder}
              onRename={onRenameFolder}
            />
          </div>
        </AccordionPrimitive.Trigger>
      </AccordionPrimitive.Header>
      <AccordionPrimitive.Content className="overflow-hidden data-[state=closed]:animate-accordion-up data-[state=open]:animate-accordion-down w-full min-w-0">
        {node.children?.map((child) => (
          <TreeNode
            key={child.id}
            node={child}
            depth={depth + 1}
            supportsFilePriority={supportsFilePriority}
            pendingFileIndices={pendingFileIndices}
            incognitoMode={incognitoMode}
            savePath={savePath}
            folderState={folderState}
            onToggleFile={onToggleFile}
            onToggleFolder={onToggleFolder}
            onRenameFile={onRenameFile}
            onRenameFolder={onRenameFolder}
          />
        ))}
      </AccordionPrimitive.Content>
    </AccordionPrimitive.Item>
  )
})

export const TorrentFileTree = memo(function TorrentFileTree({
  files,
  supportsFilePriority,
  pendingFileIndices,
  incognitoMode,
  torrentHash,
  savePath,
  onToggleFile,
  onToggleFolder,
  onRenameFile,
  onRenameFolder,
}: TorrentFileTreeProps) {
  const { nodes, allFolderIds } = useMemo(
    () => buildFileTree(files, incognitoMode, torrentHash),
    [files, incognitoMode, torrentHash]
  )

  // Start with all folders expanded
  const [folderState, setFolderState] = useState<Map<string, boolean>>(
    () => new Map(allFolderIds.map((key) => [key, true]))
  )

  // Keep folderState in sync when folder paths change (e.g., after rename)
  // - Add newly appearing folders as expanded
  // - Remove folders that no longer exist
  useEffect(() => {
    setFolderState((prev) => {
      const allFolderSet = new Set(allFolderIds)
      const next = new Map(prev)
      let changed = false

      // Remove folders that no longer exist
      for (const id of prev.keys()) {
        if (!allFolderSet.has(id)) {
          next.delete(id)
          changed = true
        }
      }

      // Add new folders as expanded by default
      for (const id of allFolderIds) {
        if (!prev.has(id)) {
          next.set(id, true)
          changed = true
        }
      }

      return changed ? next : prev
    })
  }, [allFolderIds])

  const expandedArray = useMemo(
    () =>
      Array.from(folderState)
        .filter((folder) => folder[1])
        .map((folder) => folder[0]),
    [folderState]
  )

  // Handle single file torrents (no folders)
  if (nodes.length === 1 && nodes[0].kind === "file") {
    return (
      <div className="space-y-0.5 w-full min-w-0" onContextMenu={(e) => e.preventDefault()}>
        <FileRow
          node={nodes[0]}
          depth={0}
          supportsFilePriority={supportsFilePriority}
          isPending={pendingFileIndices.has(nodes[0].file!.index)}
          incognitoMode={incognitoMode}
          savePath={savePath}
          onToggle={onToggleFile}
          onRename={onRenameFile}
        />
      </div>
    )
  }

  // Handle flat file list (multiple files, no folders)
  const hasAnyFolders = nodes.some((n) => n.kind === "folder")
  if (!hasAnyFolders) {
    return (
      <div className="space-y-0.5 w-full min-w-0" onContextMenu={(e) => e.preventDefault()}>
        {nodes.map((node) => (
          <FileRow
            key={node.id}
            node={node}
            depth={0}
            supportsFilePriority={supportsFilePriority}
            isPending={node.file ? pendingFileIndices.has(node.file.index) : false}
            incognitoMode={incognitoMode}
            savePath={savePath}
            onToggle={onToggleFile}
            onRename={onRenameFile}
          />
        ))}
      </div>
    )
  }

  return (
    <div className="w-full min-w-0" onContextMenu={(e) => e.preventDefault()}>
      <AccordionPrimitive.Root
        type="multiple"
        value={expandedArray}
        onValueChange={(value) => {
          setFolderState((prev) => {
            const valueSet = new Set(value)
            const next = new Map(prev)
            for (const id of prev.keys()) {
              next.set(id, valueSet.has(id))
            }
            return next
          })
        }}
        className="space-y-0.5 w-full min-w-0"
      >
        {nodes.map((node) => (
          <TreeNode
            key={node.id}
            node={node}
            depth={0}
            supportsFilePriority={supportsFilePriority}
            pendingFileIndices={pendingFileIndices}
            incognitoMode={incognitoMode}
            savePath={savePath}
            folderState={folderState}
            onToggleFile={onToggleFile}
            onToggleFolder={onToggleFolder}
            onRenameFile={onRenameFile}
            onRenameFolder={onRenameFolder}
          />
        ))}
      </AccordionPrimitive.Root>
    </div>
  )
})
