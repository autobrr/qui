/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger
} from "@/components/ui/context-menu"
import type { TorrentAction } from "@/hooks/useTorrentActions"
import { TORRENT_ACTIONS } from "@/hooks/useTorrentActions"
import { searchCrossSeedMatches, type CrossSeedTorrent } from "@/lib/cross-seed-utils"
import { getLinuxIsoName, getLinuxSavePath, useIncognitoMode } from "@/lib/incognito"
import { getTorrentDisplayHash } from "@/lib/torrent-utils"
import { copyTextToClipboard } from "@/lib/utils"
import type { Category, Instance, InstanceCapabilities, Torrent, TorrentFilters } from "@/types"
import {
  CheckCircle,
  Copy,
  Download,
  FastForward,
  FolderOpen,
  Gauge,
  GitBranch,
  Pause,
  Play,
  Radio,
  Search,
  Settings2,
  Sparkles,
  Sprout,
  Tag,
  Terminal,
  Trash2
} from "lucide-react"
import { memo, useCallback, useMemo, useState } from "react"
import { toast } from "sonner"
import { useQuery, useMutation } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { ExternalProgram } from "@/types"
import { CategorySubmenu } from "./CategorySubmenu"
import { QueueSubmenu } from "./QueueSubmenu"
import { RenameSubmenu } from "./RenameSubmenu"

interface TorrentContextMenuProps {
  children: React.ReactNode
  instanceId: number
  torrent: Torrent
  isSelected: boolean
  isAllSelected?: boolean
  selectedHashes: string[]
  selectedTorrents: Torrent[]
  effectiveSelectionCount: number
  onTorrentSelect?: (torrent: Torrent | null) => void
  onAction: (action: TorrentAction, hashes: string[], options?: { enable?: boolean }) => void
  onPrepareDelete: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareTags: (action: "add" | "set" | "remove", hashes: string[], torrents?: Torrent[]) => void
  onPrepareCategory: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareCreateCategory: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareShareLimit: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareSpeedLimits: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareRecheck: (hashes: string[], count?: number) => void
  onPrepareReannounce: (hashes: string[], count?: number) => void
  onPrepareLocation: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareRenameTorrent: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareRenameFile: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareRenameFolder: (hashes: string[], torrents?: Torrent[]) => void
  availableCategories?: Record<string, Category>
  onSetCategory?: (category: string, hashes: string[]) => void
  isPending?: boolean
  onExport?: (hashes: string[], torrents: Torrent[]) => Promise<void> | void
  isExporting?: boolean
  capabilities?: InstanceCapabilities
  useSubcategories?: boolean
  canCrossSeedSearch?: boolean
  onCrossSeedSearch?: (torrent: Torrent) => void
  isCrossSeedSearching?: boolean
  onFilterChange?: (filters: TorrentFilters) => void
}

export const TorrentContextMenu = memo(function TorrentContextMenu({
  children,
  instanceId: _instanceId,
  torrent,
  isSelected,
  isAllSelected = false,
  selectedHashes,
  selectedTorrents,
  effectiveSelectionCount,
  onTorrentSelect,
  onAction,
  onPrepareDelete,
  onPrepareTags,
  onPrepareShareLimit,
  onPrepareSpeedLimits,
  onPrepareRecheck,
  onPrepareReannounce,
  onPrepareLocation,
  onPrepareRenameTorrent,
  onPrepareRenameFile,
  onPrepareRenameFolder,
  availableCategories = {},
  onSetCategory,
  isPending = false,
  onExport,
  isExporting = false,
  capabilities,
  useSubcategories = false,
  canCrossSeedSearch = false,
  onCrossSeedSearch,
  isCrossSeedSearching = false,
  onFilterChange,
}: TorrentContextMenuProps) {
  const [incognitoMode] = useIncognitoMode()

  // Determine if we should use selection or just this torrent
  const useSelection = isSelected || isAllSelected

  // Memoize hashes and torrents to avoid re-creating arrays on every render
  const hashes = useMemo(() =>
    useSelection ? selectedHashes : [torrent.hash],
  [useSelection, selectedHashes, torrent.hash]
  )

  const torrents = useMemo(() =>
    useSelection ? selectedTorrents : [torrent],
  [useSelection, selectedTorrents, torrent]
  )

  const count = isAllSelected ? effectiveSelectionCount : hashes.length

  // State for cross-seed search
  const [isSearchingCrossSeeds, setIsSearchingCrossSeeds] = useState(false)
  
  const handleFilterCrossSeeds = useCallback(async () => {
    if (!onFilterChange || isSearchingCrossSeeds) {
      return
    }

    // Early guard: only allow for single torrent selection
    if (torrents.length !== 1) {
      toast.info("Cross-seed filtering only works with a single selected torrent")
      return
    }

    setIsSearchingCrossSeeds(true)
    toast.info("Identifying cross-seeded torrents...")

    try {
      // Get the selected torrent
      const selectedTorrent = torrents[0]
      
      // Fetch all instances
      const allInstancesData = await api.getInstances()
      
      // Fetch current torrent files for the selected torrent
      const torrentFilesData = await api.getTorrentFiles(_instanceId, selectedTorrent.hash)
      
      // Collect all cross-seed matches across all instances
      const allMatches: CrossSeedTorrent[] = []
      
      if (allInstancesData && Array.isArray(allInstancesData)) {
        // Timeout wrapper for individual instance searches
        const searchWithTimeout = async (instance: Instance, timeoutMs: number = 15000) => {
          let timerHandle: any
          const timeoutPromise = new Promise<CrossSeedTorrent[]>((_, reject) => {
            timerHandle = setTimeout(() => reject(new Error(`Timeout after ${timeoutMs}ms`)), timeoutMs)
          })
          
          const searchPromise = searchCrossSeedMatches(
            selectedTorrent,
            instance,
            _instanceId,
            torrentFilesData || [],
            selectedTorrent.infohash_v1,
            selectedTorrent.infohash_v2
          )
          
          try {
            return await Promise.race([searchPromise, timeoutPromise])
          } catch (error) {
            if (error instanceof Error && error.message.includes('Timeout')) {
              console.warn(`Cross-seed search timed out for instance ${instance.name} after ${timeoutMs}ms`)
              toast.warning(`Search timed out for instance "${instance.name}"`, {
                description: "Continuing with other instances...",
                duration: 3000
              })
            } else {
              console.warn(`Failed to search cross-seeds on instance ${instance.name}:`, error)
            }
            return []
          } finally {
            if (timerHandle) {
              clearTimeout(timerHandle)
            }
          }
        }

        // Create search promises with timeout handling
        const searchPromises = allInstancesData.map(instance => searchWithTimeout(instance))
        
        // Use Promise.allSettled to get partial results even if some instances fail/timeout
        const searchResults = await Promise.allSettled(searchPromises)
        
        // Process results and collect successful matches
        let successfulSearches = 0
        let timedOutSearches = 0
        let failedSearches = 0
        
        searchResults.forEach((result) => {
          if (result.status === 'fulfilled') {
            allMatches.push(...result.value)
            successfulSearches++
          } else {
            if (result.reason?.message?.includes('Timeout')) {
              timedOutSearches++
            } else {
              failedSearches++
            }
          }
        })

        // Show summary toast if there were any issues
        if (timedOutSearches > 0 || failedSearches > 0) {
          toast.info(`Search completed with partial results`, {
            description: `${successfulSearches}/${allInstancesData.length} instances searched successfully. ${timedOutSearches} timed out, ${failedSearches} failed.`,
            duration: 5000
          })
        }
      }
      
      if (allMatches.length > 0) {
        // Create expression from cross-seed matches using proper CEL/expr syntax
        const hashConditions = allMatches.map(match => `Hash == "${match.hash}"`)
        
        // Also include the original torrent's hash to show it in the filtered results
        hashConditions.push(`Hash == "${selectedTorrent.hash}"`)
        
        // Remove duplicates (in case the original torrent is already in the matches)
        const uniqueConditions = [...new Set(hashConditions)]
        const hashExpression = uniqueConditions.join(' || ')

        // Create new filters with expression - clear all other filters to ensure clean filtering
        const newFilters: TorrentFilters = {
          status: [],
          excludeStatus: [],
          categories: [],
          excludeCategories: [],
          tags: [],
          excludeTags: [],
          trackers: [],
          excludeTrackers: [],
          expr: hashExpression
        }

        // Apply the filter immediately - the delay was unnecessary
        onFilterChange(newFilters)
        toast.success(`Found ${allMatches.length} cross-seeded torrents (showing ${uniqueConditions.length} total)`)
      } else {
        toast.info("No cross-seeded torrents found")
      }
    } catch (error) {
      console.error('Failed to identify cross-seeded torrents:', error)
      toast.error("Failed to identify cross-seeded torrents")
    } finally {
      setIsSearchingCrossSeeds(false)
    }
  }, [onFilterChange, torrents, isSearchingCrossSeeds, _instanceId])

  const copyToClipboard = useCallback(async (text: string, type: "name" | "hash" | "full path", itemCount: number) => {
    try {
      await copyTextToClipboard(text)
      const pluralTypes: Record<"name" | "hash" | "full path", string> = {
        name: "names",
        hash: "hashes",
        "full path": "full paths",
      }
      const label = itemCount > 1 ? pluralTypes[type] : type
      toast.success(`Torrent ${label} copied to clipboard`)
    } catch {
      toast.error("Failed to copy to clipboard")
    }
  }, [])

  const handleCopyNames = useCallback(() => {
    const values = torrents
      .map(t => incognitoMode ? getLinuxIsoName(t.hash) : t.name)
      .map(value => (value ?? "").trim())
      .filter(Boolean)

    if (values.length === 0) {
      toast.error("Name not available")
      return
    }

    void copyToClipboard(values.join("\n"), "name", values.length)
  }, [copyToClipboard, incognitoMode, torrents])

  const handleCopyHashes = useCallback(() => {
    const values = torrents
      .map(t => getTorrentDisplayHash(t) || t.hash || "")
      .map(value => value.trim())
      .filter(Boolean)

    if (values.length === 0) {
      toast.error("Hash not available")
      return
    }
    void copyToClipboard(values.join("\n"), "hash", values.length)
  }, [copyToClipboard, torrents])

  const handleCopyFullPaths = useCallback(() => {
    const values = torrents
      .map(t => {
        const name = incognitoMode ? getLinuxIsoName(t.hash) : t.name
        const savePath = incognitoMode ? getLinuxSavePath(t.hash) : t.save_path
        if (!name || !savePath) {
          return ""
        }
        return `${savePath}/${name}`
      })
      .map(value => value.trim())
      .filter(Boolean)

    if (values.length === 0) {
      toast.error("Full path not available")
      return
    }

    void copyToClipboard(values.join("\n"), "full path", values.length)
  }, [copyToClipboard, incognitoMode, torrents])

  const handleExport = useCallback(() => {
    if (!onExport) {
      return
    }
    void onExport(hashes, torrents)
  }, [hashes, onExport, torrents])

  const forceStartStates = torrents.map(t => t.force_start)
  const allForceStarted = forceStartStates.length > 0 && forceStartStates.every(state => state === true)
  const allForceDisabled = forceStartStates.length > 0 && forceStartStates.every(state => state === false)
  const forceStartMixed = forceStartStates.length > 0 && !allForceStarted && !allForceDisabled

  // TMM state calculation
  const tmmStates = torrents.map(t => t.auto_tmm)
  const allEnabled = tmmStates.length > 0 && tmmStates.every(state => state === true)
  const allDisabled = tmmStates.length > 0 && tmmStates.every(state => state === false)
  const mixed = tmmStates.length > 0 && !allEnabled && !allDisabled

  const handleQueueAction = useCallback((action: "topPriority" | "increasePriority" | "decreasePriority" | "bottomPriority") => {
    onAction(action as TorrentAction, hashes)
  }, [onAction, hashes])

  const handleForceStartToggle = useCallback((enable: boolean) => {
    onAction(TORRENT_ACTIONS.FORCE_START, hashes, { enable })
  }, [onAction, hashes])

  const handleSetCategory = useCallback((category: string) => {
    if (onSetCategory) {
      onSetCategory(category, hashes)
    }
  }, [onSetCategory, hashes])

  const supportsTorrentExport = capabilities?.supportsTorrentExport ?? true

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        {children}
      </ContextMenuTrigger>
      <ContextMenuContent
        alignOffset={8}
        collisionPadding={10}
        className="ml-2"
      >
        <ContextMenuItem onClick={() => onTorrentSelect?.(torrent)}>
          View Details
        </ContextMenuItem>
        {onFilterChange && (
          <ContextMenuItem
            onClick={handleFilterCrossSeeds}
            disabled={isPending || isSearchingCrossSeeds || count > 1}
            title={count > 1 ? "Cross-seed filtering only works with a single selected torrent" : undefined}
          >
            <GitBranch className="mr-2 h-4 w-4" />
            {count > 1 ? (
              <span className="text-muted-foreground">Filter Cross-Seeds (single selection only)</span>
            ) : (
              <>Filter Cross-Seeds</>
            )}
            {isSearchingCrossSeeds && <span className="ml-1 text-xs text-muted-foreground">...</span>}
          </ContextMenuItem>
        )}
        <ContextMenuSeparator />
        <ContextMenuItem
          onClick={() => onAction(TORRENT_ACTIONS.RESUME, hashes)}
          disabled={isPending}
        >
          <Play className="mr-2 h-4 w-4" />
          Resume {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        {forceStartMixed ? (
          <>
            <ContextMenuItem
              onClick={() => handleForceStartToggle(true)}
              disabled={isPending}
            >
              <FastForward className="mr-2 h-4 w-4" />
              Force Start {count > 1 ? `(${count} Mixed)` : "(Mixed)"}
            </ContextMenuItem>
            <ContextMenuItem
              onClick={() => handleForceStartToggle(false)}
              disabled={isPending}
            >
              <FastForward className="mr-2 h-4 w-4" />
              Disable Force Start {count > 1 ? `(${count} Mixed)` : "(Mixed)"}
            </ContextMenuItem>
          </>
        ) : (
          <ContextMenuItem
            onClick={() => handleForceStartToggle(!allForceStarted)}
            disabled={isPending}
          >
            <FastForward className="mr-2 h-4 w-4" />
            {allForceStarted
              ? `Disable Force Start ${count > 1 ? `(${count})` : ""}`
              : `Force Start ${count > 1 ? `(${count})` : ""}`}
          </ContextMenuItem>
        )}
        <ContextMenuItem
          onClick={() => onAction(TORRENT_ACTIONS.PAUSE, hashes)}
          disabled={isPending}
        >
          <Pause className="mr-2 h-4 w-4" />
          Pause {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onPrepareRecheck(hashes, count)}
          disabled={isPending}
        >
          <CheckCircle className="mr-2 h-4 w-4" />
          Force Recheck {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onPrepareReannounce(hashes, count)}
          disabled={isPending}
        >
          <Radio className="mr-2 h-4 w-4" />
          Reannounce {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <ContextMenuSeparator />
        <QueueSubmenu
          type="context"
          hashCount={count}
          onQueueAction={handleQueueAction}
          isPending={isPending}
        />
        <ContextMenuSeparator />
        {canCrossSeedSearch && (
          <ContextMenuItem
            onClick={() => onCrossSeedSearch?.(torrent)}
            disabled={isPending || isCrossSeedSearching}
          >
            <Search className="mr-2 h-4 w-4" />
            Search Cross-Seeds
          </ContextMenuItem>
        )}
        {canCrossSeedSearch && <ContextMenuSeparator />}
        <ContextMenuItem
          onClick={() => onPrepareTags("add", hashes, torrents)}
          disabled={isPending}
        >
          <Tag className="mr-2 h-4 w-4" />
          Add Tags {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onPrepareTags("set", hashes, torrents)}
          disabled={isPending}
        >
          <Tag className="mr-2 h-4 w-4" />
          Replace Tags {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <CategorySubmenu
          type="context"
          hashCount={count}
          availableCategories={availableCategories}
          onSetCategory={handleSetCategory}
          isPending={isPending}
          currentCategory={torrent.category}
          useSubcategories={useSubcategories}
        />
        <ContextMenuItem
          onClick={() => onPrepareLocation(hashes, torrents)}
          disabled={isPending}
        >
          <FolderOpen className="mr-2 h-4 w-4" />
          Set Location {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <RenameSubmenu
          type="context"
          hashCount={count}
          onRenameTorrent={() => onPrepareRenameTorrent(hashes, torrents)}
          onRenameFile={() => onPrepareRenameFile(hashes, torrents)}
          onRenameFolder={() => onPrepareRenameFolder(hashes, torrents)}
          isPending={isPending}
          capabilities={capabilities}
        />
        <ContextMenuSeparator />
        <ContextMenuItem
          onClick={() => onPrepareShareLimit(hashes, torrents)}
          disabled={isPending}
        >
          <Sprout className="mr-2 h-4 w-4" />
          Set Share Limits {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onPrepareSpeedLimits(hashes, torrents)}
          disabled={isPending}
        >
          <Gauge className="mr-2 h-4 w-4" />
          Set Speed Limits {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <ContextMenuSeparator />
        {mixed ? (
          <>
            <ContextMenuItem
              onClick={() => onAction(TORRENT_ACTIONS.TOGGLE_AUTO_TMM, hashes, { enable: true })}
              disabled={isPending}
            >
              <Sparkles className="mr-2 h-4 w-4" />
              Enable TMM {count > 1 ? `(${count} Mixed)` : "(Mixed)"}
            </ContextMenuItem>
            <ContextMenuItem
              onClick={() => onAction(TORRENT_ACTIONS.TOGGLE_AUTO_TMM, hashes, { enable: false })}
              disabled={isPending}
            >
              <Settings2 className="mr-2 h-4 w-4" />
              Disable TMM {count > 1 ? `(${count} Mixed)` : "(Mixed)"}
            </ContextMenuItem>
          </>
        ) : (
          <ContextMenuItem
            onClick={() => onAction(TORRENT_ACTIONS.TOGGLE_AUTO_TMM, hashes, { enable: !allEnabled })}
            disabled={isPending}
          >
            {allEnabled ? (
              <>
                <Settings2 className="mr-2 h-4 w-4" />
                Disable TMM {count > 1 ? `(${count})` : ""}
              </>
            ) : (
              <>
                <Sparkles className="mr-2 h-4 w-4" />
                Enable TMM {count > 1 ? `(${count})` : ""}
              </>
            )}
          </ContextMenuItem>
        )}
        <ContextMenuSeparator />
        <ExternalProgramsSubmenu instanceId={_instanceId} hashes={hashes} />
        {supportsTorrentExport && (
          <ContextMenuItem
            onClick={handleExport}
            disabled={isExporting}
          >
            <Download className="mr-2 h-4 w-4" />
            {count > 1 ? `Export Torrents (${count})` : "Export Torrent"}
          </ContextMenuItem>
        )}
        <ContextMenuSub>
          <ContextMenuSubTrigger>
            <Copy className="mr-4 h-4 w-4" />
            Copy...
          </ContextMenuSubTrigger>
          <ContextMenuSubContent>
            <ContextMenuItem onClick={handleCopyNames}>
              Copy Name
            </ContextMenuItem>
            <ContextMenuItem onClick={handleCopyHashes}>
              Copy Hash
            </ContextMenuItem>
            <ContextMenuItem onClick={handleCopyFullPaths}>
              Copy Full Path
            </ContextMenuItem>
          </ContextMenuSubContent>
        </ContextMenuSub>
        <ContextMenuSeparator />
        <ContextMenuItem
          onClick={() => onPrepareDelete(hashes, torrents)}
          disabled={isPending}
          className="text-destructive"
        >
          <Trash2 className="mr-2 h-4 w-4" />
          Delete {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
})

interface ExternalProgramsSubmenuProps {
  instanceId: number
  hashes: string[]
}

function ExternalProgramsSubmenu({ instanceId, hashes }: ExternalProgramsSubmenuProps) {
  const { data: programs, isLoading } = useQuery({
    queryKey: ["externalPrograms", "enabled"],
    queryFn: () => api.listExternalPrograms(),
    select: (data) => data.filter(p => p.enabled),
    staleTime: 60 * 1000, // 1 minute
  })

  // Types derived from API for strong typing
  type ExecResp = Awaited<ReturnType<typeof api.executeExternalProgram>>
  type ExecVars = { program: ExternalProgram; instanceId: number; hashes: string[] }

  const executeMutation = useMutation<ExecResp, Error, ExecVars>({
    mutationFn: async ({ program, instanceId, hashes }) =>
      api.executeExternalProgram({
        program_id: program.id,
        instance_id: instanceId,
        hashes,
      }),
    onSuccess: (response) => {
      const successCount = response.results.filter(r => r.success).length
      const failureCount = response.results.length - successCount

      if (failureCount === 0) {
        toast.success(`External program executed successfully for ${successCount} torrent(s)`)
      } else if (successCount === 0) {
        toast.error(`Failed to execute external program for all ${failureCount} torrent(s)`)
      } else {
        toast.warning(`Executed for ${successCount} torrent(s), failed for ${failureCount}`)
      }

      // Log detailed errors in development only to avoid leaking PII/paths in production
      if (import.meta.env.DEV) {
        response.results.forEach(r => {
          if (!r.success && r.error) console.error(`External program failed for ${r.hash}:`, r.error)
        })
      }
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : String(error)
      toast.error(`Failed to execute external program: ${message}`)
    },
  })

  const handleExecute = useCallback((program: ExternalProgram) => {
    executeMutation.mutate({ program, instanceId, hashes })
  }, [executeMutation, instanceId, hashes])

  if (isLoading) {
    return (
      <ContextMenuItem disabled>
        Loading programs...
      </ContextMenuItem>
    )
  }

  // programs is already filtered to enabled by select
  if (!programs || programs.length === 0) {
    return null // Don't show the submenu if no programs are enabled
  }

  return (
    <ContextMenuSub>
      <ContextMenuSubTrigger>
        <Terminal className="mr-4 h-4 w-4" />
        External Programs
      </ContextMenuSubTrigger>
      <ContextMenuSubContent>
        {programs.map(program => (
          <ContextMenuItem
            key={program.id}
            onClick={() => handleExecute(program)}
            disabled={executeMutation.isPending}
          >
            {program.name}
          </ContextMenuItem>
        ))}
      </ContextMenuSubContent>
    </ContextMenuSub>
  )
}
