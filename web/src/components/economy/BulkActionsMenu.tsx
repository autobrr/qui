/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { api } from "@/lib/api"
import type { EconomyScore } from "@/types"
import { useMutation } from "@tanstack/react-query"
import {
  ChevronDown,
  Pause,
  Play,
  Trash2,
  RefreshCw,
  TrendingUp,
  TrendingDown,
  Tag,
  Folder,
} from "lucide-react"
import { toast } from "sonner"

interface BulkActionsMenuProps {
  instanceId: number
  selectedTorrents: EconomyScore[]
  onActionComplete: () => void
  onClearSelection: () => void
}

export function BulkActionsMenu({
  instanceId,
  selectedTorrents,
  onActionComplete,
  onClearSelection,
}: BulkActionsMenuProps) {
  const bulkActionMutation = useMutation({
    mutationFn: async (action: {
      action: string
      deleteFiles?: boolean
    }) => {
      const hashes = selectedTorrents.map((t) => t.hash)
      await api.bulkAction(instanceId, {
        hashes,
        action: action.action as any,
        deleteFiles: action.deleteFiles,
      })
    },
    onSuccess: (_data, variables) => {
      toast.success(`Action "${variables.action}" completed for ${selectedTorrents.length} torrent(s)`)
      onActionComplete()
      onClearSelection()
    },
    onError: (error: Error, variables) => {
      toast.error(`Failed to ${variables.action}: ${error.message}`)
    },
  })

  const handleAction = (action: string, deleteFiles?: boolean) => {
    bulkActionMutation.mutate({ action, deleteFiles })
  }

  const totalSize = selectedTorrents.reduce((sum, t) => sum + t.size, 0)
  const formatBytes = (bytes: number) => {
    if (bytes === 0) return "0 B"
    const k = 1024
    const sizes = ["B", "KB", "MB", "GB", "TB"]
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          disabled={bulkActionMutation.isPending}
        >
          Actions ({selectedTorrents.length})
          <ChevronDown className="ml-2 h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-56">
        <DropdownMenuLabel>
          <div className="flex flex-col gap-1">
            <span>Bulk Actions</span>
            <span className="text-xs text-muted-foreground font-normal">
              {selectedTorrents.length} torrent(s) • {formatBytes(totalSize)}
            </span>
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        
        <DropdownMenuItem
          onClick={() => handleAction("pause")}
          disabled={bulkActionMutation.isPending}
        >
          <Pause className="mr-2 h-4 w-4" />
          Pause
        </DropdownMenuItem>
        
        <DropdownMenuItem
          onClick={() => handleAction("resume")}
          disabled={bulkActionMutation.isPending}
        >
          <Play className="mr-2 h-4 w-4" />
          Resume
        </DropdownMenuItem>
        
        <DropdownMenuItem
          onClick={() => handleAction("recheck")}
          disabled={bulkActionMutation.isPending}
        >
          <RefreshCw className="mr-2 h-4 w-4" />
          Recheck
        </DropdownMenuItem>
        
        <DropdownMenuItem
          onClick={() => handleAction("reannounce")}
          disabled={bulkActionMutation.isPending}
        >
          <RefreshCw className="mr-2 h-4 w-4" />
          Reannounce
        </DropdownMenuItem>
        
        <DropdownMenuSeparator />
        
        <DropdownMenuItem
          onClick={() => handleAction("increasePriority")}
          disabled={bulkActionMutation.isPending}
        >
          <TrendingUp className="mr-2 h-4 w-4" />
          Increase Priority
        </DropdownMenuItem>
        
        <DropdownMenuItem
          onClick={() => handleAction("decreasePriority")}
          disabled={bulkActionMutation.isPending}
        >
          <TrendingDown className="mr-2 h-4 w-4" />
          Decrease Priority
        </DropdownMenuItem>
        
        <DropdownMenuSeparator />
        
        <DropdownMenuItem
          onClick={() => handleAction("delete", false)}
          disabled={bulkActionMutation.isPending}
          className="text-orange-600 focus:text-orange-600"
        >
          <Trash2 className="mr-2 h-4 w-4" />
          Delete (Keep Files)
        </DropdownMenuItem>
        
        <DropdownMenuItem
          onClick={() => handleAction("delete", true)}
          disabled={bulkActionMutation.isPending}
          className="text-red-600 focus:text-red-600"
        >
          <Trash2 className="mr-2 h-4 w-4" />
          Delete with Files
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
