import { memo, useState, useCallback } from 'react'
import type { ChangeEvent } from 'react'
import { useMutation, useQueryClient, useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { toast } from 'sonner'
import { applyOptimisticUpdates } from '@/lib/torrent-state-utils'
import { getCommonTags, getCommonCategory } from '@/lib/torrent-utils'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { ChevronDown, Play, Pause, Trash2, CheckCircle, Tag, Folder, Radio, ArrowUp, ArrowDown, ChevronsUp, ChevronsDown, Settings2, Sparkles } from 'lucide-react'
import { SetTagsDialog, SetCategoryDialog } from './TorrentDialogs'

interface TorrentActionsProps {
  instanceId: number
  selectedHashes: string[]
  selectedTorrents?: any[] // Torrent type from parent
  onComplete?: () => void
}

export const TorrentActions = memo(function TorrentActions({ instanceId, selectedHashes, selectedTorrents = [], onComplete }: TorrentActionsProps) {
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [deleteFiles, setDeleteFiles] = useState(false)
  const [showTagsDialog, setShowTagsDialog] = useState(false)
  const [showCategoryDialog, setShowCategoryDialog] = useState(false)
  const queryClient = useQueryClient()

  // Fetch available tags
  const { data: availableTags = [] } = useQuery({
    queryKey: ['tags', instanceId],
    queryFn: () => api.getTags(instanceId),
    staleTime: 60000,
  })

  // Fetch available categories
  const { data: availableCategories = {} } = useQuery({
    queryKey: ['categories', instanceId],
    queryFn: () => api.getCategories(instanceId),
    staleTime: 60000,
  })

  const mutation = useMutation({
    mutationFn: (data: {
      action: 'pause' | 'resume' | 'delete' | 'recheck' | 'reannounce' | 'increasePriority' | 'decreasePriority' | 'topPriority' | 'bottomPriority' | 'addTags' | 'removeTags' | 'setTags' | 'setCategory' | 'toggleAutoTMM'
      deleteFiles?: boolean
      tags?: string
      category?: string
      enable?: boolean
    }) => {
      return api.bulkAction(instanceId, {
        hashes: selectedHashes,
        action: data.action,
        deleteFiles: data.deleteFiles,
        tags: data.tags,
        category: data.category,
        enable: data.enable,
      })
    },
  onSuccess: async (_: unknown, variables: any) => {
      // For delete operations, force immediate refetch
      if (variables.action === 'delete') {
        // Remove the query data to force immediate UI update
        queryClient.removeQueries({
          queryKey: ['torrents-list', instanceId],
          exact: false
        })
        // Also remove counts query
        queryClient.removeQueries({
          queryKey: ['torrent-counts', instanceId],
          exact: false
        })
        
        // Then trigger a refetch
        await queryClient.refetchQueries({
          queryKey: ['torrents-list', instanceId],
          exact: false
        })
        await queryClient.refetchQueries({
          queryKey: ['torrent-counts', instanceId],
          exact: false
        })
        onComplete?.()
      } else {
        // Apply optimistic updates for actions that change visible state
        const optimisticActions = ['pause', 'resume', 'delete', 'deleteWithFiles', 'recheck', 'setCategory', 'addTags', 'removeTags', 'setTags', 'toggleAutoTMM']
        
        if (optimisticActions.includes(variables.action)) {
          // Get all cached queries for this instance
          const cache = queryClient.getQueryCache()
          const queries = cache.findAll({
            queryKey: ['torrents-list', instanceId],
            exact: false
          })
          
          // Build payload for the action
          const payload = {
            category: variables.category,
            tags: variables.tags,
            enable: variables.enable,
            deleteFiles: variables.deleteFiles
          }
          
          // Optimistically update torrent states in all cached queries
          queries.forEach((query: any) => {
            queryClient.setQueryData(query.queryKey, (oldData: any) => {
              if (!oldData?.torrents) return oldData
              
              // Check if this query has a status filter in its key
              // Query key structure: ['torrents-list', instanceId, currentPage, filters, search]
              const queryKey = query.queryKey as any[]
              const filters = queryKey[3] // filters is at index 3
              const statusFilters = filters?.status || []
              
              // Apply optimistic updates using our utility function
              const { torrents: updatedTorrents } = applyOptimisticUpdates(
                oldData.torrents,
                selectedHashes,
                variables.action,
                statusFilters,
                payload
              )
              
              return {
                ...oldData,
                torrents: updatedTorrents,
                total: updatedTorrents.length,
                totalCount: updatedTorrents.length
              }
            })
          })
          
          // Note: torrent-counts are handled server-side now, no need for optimistic updates
        }
        
        // For other operations, add delay to allow qBittorrent to process
        // Resume operations need more time for state transition
        const delay = variables.action === 'resume' ? 2000 : 1000
        
        setTimeout(() => {
          // Always invalidate to get the real state from server
          queryClient.invalidateQueries({ 
            queryKey: ['torrents-list', instanceId],
            exact: false 
          })
          queryClient.invalidateQueries({ 
            queryKey: ['torrent-counts', instanceId],
            exact: false 
          })
        }, delay)
        onComplete?.()
      }
      
      // Show success toast
      const count = selectedHashes.length
      const torrentText = count === 1 ? 'torrent' : 'torrents'
      
      switch (variables.action) {
        case 'resume':
          toast.success(`Resumed ${count} ${torrentText}`)
          break
        case 'pause':
          toast.success(`Paused ${count} ${torrentText}`)
          break
        case 'delete':
          toast.success(`Deleted ${count} ${torrentText}${variables.deleteFiles ? ' and files' : ''}`)
          break
        case 'recheck':
          toast.success(`Started recheck for ${count} ${torrentText}`)
          break
        case 'reannounce':
          toast.success(`Reannounced ${count} ${torrentText}`)
          break
        case 'increasePriority':
          toast.success(`Increased priority for ${count} ${torrentText}`)
          break
        case 'decreasePriority':
          toast.success(`Decreased priority for ${count} ${torrentText}`)
          break
        case 'topPriority':
          toast.success(`Set ${count} ${torrentText} to top priority`)
          break
        case 'bottomPriority':
          toast.success(`Set ${count} ${torrentText} to bottom priority`)
          break
        case 'addTags':
          toast.success(`Added tags to ${count} ${torrentText}`)
          break
        case 'removeTags':
          toast.success(`Removed tags from ${count} ${torrentText}`)
          break
        case 'setTags':
          toast.success(`Updated tags for ${count} ${torrentText}`)
          break
        case 'setCategory':
          toast.success(`Set category for ${count} ${torrentText}`)
          break
        case 'toggleAutoTMM':
          toast.success(`${variables.enable ? 'Enabled' : 'Disabled'} Auto TMM for ${count} ${torrentText}`)
          break
      }
    },
  onError: (error: any, variables: any) => {
      const count = selectedHashes.length
      const torrentText = count === 1 ? 'torrent' : 'torrents'
      const actionText = variables.action === 'recheck' ? 'recheck' : variables.action
      
      toast.error(`Failed to ${actionText} ${count} ${torrentText}`, {
        description: error.message || 'An unexpected error occurred'
      })
    },
  })

  const handleDelete = useCallback(async () => {
    await mutation.mutateAsync({ action: 'delete', deleteFiles })
    setShowDeleteDialog(false)
    setDeleteFiles(false)
  }, [mutation, deleteFiles])

  const handleSetTags = useCallback(async (tags: string[]) => {
    // Use setTags action (with fallback to addTags for older versions)
    // The backend will handle the version check
    try {
      await mutation.mutateAsync({ action: 'setTags', tags: tags.join(',') })
    } catch (error: any) {
      // If setTags fails due to version requirement, fall back to addTags
      if (error.message?.includes('requires qBittorrent')) {
        await mutation.mutateAsync({ action: 'addTags', tags: tags.join(',') })
      } else {
        throw error
      }
    }
    
    setShowTagsDialog(false)
  }, [mutation])

  const handleSetCategory = useCallback(async (category: string) => {
    await mutation.mutateAsync({ action: 'setCategory', category })
    setShowCategoryDialog(false)
  }, [mutation])

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" disabled={selectedHashes.length === 0}>
            Actions ({selectedHashes.length})
            <ChevronDown className="ml-2 h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            onClick={() => mutation.mutate({ action: 'resume' })}
            disabled={mutation.isPending}
          >
            <Play className="mr-2 h-4 w-4" />
            Resume
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => mutation.mutate({ action: 'pause' })}
            disabled={mutation.isPending}
          >
            <Pause className="mr-2 h-4 w-4" />
            Pause
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => mutation.mutate({ action: 'recheck' })}
            disabled={mutation.isPending}
          >
            <CheckCircle className="mr-2 h-4 w-4" />
            Force Recheck
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => mutation.mutate({ action: 'reannounce' })}
            disabled={mutation.isPending}
          >
            <Radio className="mr-2 h-4 w-4" />
            Reannounce
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onClick={() => mutation.mutate({ action: 'topPriority' })}
            disabled={mutation.isPending}
          >
            <ChevronsUp className="mr-2 h-4 w-4" />
            Top Priority
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => mutation.mutate({ action: 'increasePriority' })}
            disabled={mutation.isPending}
          >
            <ArrowUp className="mr-2 h-4 w-4" />
            Increase Priority
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => mutation.mutate({ action: 'decreasePriority' })}
            disabled={mutation.isPending}
          >
            <ArrowDown className="mr-2 h-4 w-4" />
            Decrease Priority
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => mutation.mutate({ action: 'bottomPriority' })}
            disabled={mutation.isPending}
          >
            <ChevronsDown className="mr-2 h-4 w-4" />
            Bottom Priority
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onClick={() => setShowTagsDialog(true)}
            disabled={mutation.isPending}
          >
            <Tag className="mr-2 h-4 w-4" />
            Set Tags
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => setShowCategoryDialog(true)}
            disabled={mutation.isPending}
          >
            <Folder className="mr-2 h-4 w-4" />
            Set Category
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          {(() => {
            // Check TMM state across selected torrents
            const tmmStates = selectedTorrents?.map(t => t.auto_tmm) ?? []
            const allEnabled = tmmStates.length > 0 && tmmStates.every(state => state === true)
            const allDisabled = tmmStates.length > 0 && tmmStates.every(state => state === false)
            const mixed = tmmStates.length > 0 && !allEnabled && !allDisabled
            
            if (mixed) {
              return (
                <>
                  <DropdownMenuItem
                    onClick={() => mutation.mutate({ action: 'toggleAutoTMM', enable: true })}
                    disabled={mutation.isPending}
                  >
                    <Sparkles className="mr-2 h-4 w-4" />
                    Enable TMM (Mixed)
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onClick={() => mutation.mutate({ action: 'toggleAutoTMM', enable: false })}
                    disabled={mutation.isPending}
                  >
                    <Settings2 className="mr-2 h-4 w-4" />
                    Disable TMM (Mixed)
                  </DropdownMenuItem>
                </>
              )
            }
            
            return (
              <DropdownMenuItem
                onClick={() => mutation.mutate({ action: 'toggleAutoTMM', enable: !allEnabled })}
                disabled={mutation.isPending}
              >
                {allEnabled ? (
                  <>
                    <Settings2 className="mr-2 h-4 w-4" />
                    Disable TMM
                  </>
                ) : (
                  <>
                    <Sparkles className="mr-2 h-4 w-4" />
                    Enable TMM
                  </>
                )}
              </DropdownMenuItem>
            )
          })()}
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onClick={() => setShowDeleteDialog(true)}
            disabled={mutation.isPending}
            className="text-destructive"
          >
            <Trash2 className="mr-2 h-4 w-4" />
            Delete
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {selectedHashes.length} torrent(s)?</AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. The torrents will be removed from qBittorrent.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex items-center space-x-2 py-4">
            <input
              type="checkbox"
              id="deleteFiles"
              checked={deleteFiles}
              onChange={(e: ChangeEvent<HTMLInputElement>) => setDeleteFiles(e.target.checked)}
              className="rounded border-input"
            />
            <label htmlFor="deleteFiles" className="text-sm font-medium">
              Also delete files from disk
            </label>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Set Tags Dialog */}
      <SetTagsDialog
        open={showTagsDialog}
        onOpenChange={setShowTagsDialog}
        availableTags={availableTags}
        hashCount={selectedHashes.length}
        onConfirm={handleSetTags}
        isPending={mutation.isPending}
        initialTags={getCommonTags(selectedTorrents)}
      />

      {/* Set Category Dialog */}
      <SetCategoryDialog
        open={showCategoryDialog}
        onOpenChange={setShowCategoryDialog}
        availableCategories={availableCategories}
        hashCount={selectedHashes.length}
        onConfirm={handleSetCategory}
        isPending={mutation.isPending}
        initialCategory={getCommonCategory(selectedTorrents)}
      />
    </>
  )
})