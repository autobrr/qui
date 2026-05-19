/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  ContextMenuItem,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger
} from "@/components/ui/context-menu"
import {
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger
} from "@/components/ui/dropdown-menu"
import type { InstanceCapabilities } from "@/types"
import { FilePen, FolderPen, Pencil } from "lucide-react"
import { memo } from "react"

type MenuKind = "context" | "dropdown"

interface RenameSubmenuProps {
  type: MenuKind
  hashCount: number
  onRenameTorrent: () => void
  onRenameFile: () => void
  onRenameFolder: () => void
  isPending?: boolean
  capabilities?: InstanceCapabilities
}

export const RenameSubmenu = memo(function RenameSubmenu({
  type,
  hashCount,
  onRenameTorrent,
  onRenameFile,
  onRenameFolder,
  isPending = false,
  capabilities,
}: RenameSubmenuProps) {
  const Sub = type === "context" ? ContextMenuSub : DropdownMenuSub
  const SubTrigger = type === "context" ? ContextMenuSubTrigger : DropdownMenuSubTrigger
  const SubContent = type === "context" ? ContextMenuSubContent : DropdownMenuSubContent
  const MenuItem = type === "context" ? ContextMenuItem : DropdownMenuItem

  const disableRename = isPending || hashCount !== 1
  const supportsRenameTorrent = capabilities?.supportsRenameTorrent ?? true
  const supportsRenameFile = capabilities?.supportsRenameFile ?? true
  const supportsRenameFolder = capabilities?.supportsRenameFolder ?? true

  // Hide entire submenu if no rename operations are supported
  const hasAnyRenameSupport = supportsRenameTorrent || supportsRenameFile || supportsRenameFolder
  if (!hasAnyRenameSupport) {
    return null
  }

  return (
    <Sub>
      <SubTrigger disabled={disableRename}>
        <Pencil className="mr-4 h-4 w-4" />
        Rename
      </SubTrigger>
      <SubContent>
        {supportsRenameTorrent && (
          <MenuItem onClick={onRenameTorrent} disabled={disableRename}>
            <Pencil className="mr-2 h-4 w-4" />
            Rename Torrent
          </MenuItem>
        )}
        {supportsRenameFile && (
          <MenuItem onClick={onRenameFile} disabled={disableRename}>
            <FilePen className="mr-2 h-4 w-4" />
            Rename File
          </MenuItem>
        )}
        {supportsRenameFolder && (
          <MenuItem onClick={onRenameFolder} disabled={disableRename}>
            <FolderPen className="mr-2 h-4 w-4" />
            Rename Folder
          </MenuItem>
        )}
      </SubContent>
    </Sub>
  )
})
