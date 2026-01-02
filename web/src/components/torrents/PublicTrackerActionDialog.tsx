/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Label } from "@/components/ui/label"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { usePublicTrackerAction, usePublicTrackerSettings, useRefreshPublicTrackerList } from "@/hooks/usePublicTrackers"
import type { PruneMode, Torrent } from "@/types"
import { AlertCircle, RefreshCw } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"

interface PublicTrackerActionDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  hashes: string[]
  torrents: Torrent[]
}

export function PublicTrackerActionDialog({
  open,
  onOpenChange,
  instanceId,
  hashes,
  torrents,
}: PublicTrackerActionDialogProps) {
  const [pruneMode, setPruneMode] = useState<PruneMode>("dead")

  const { data: settings, isLoading: settingsLoading } = usePublicTrackerSettings()
  const refreshMutation = useRefreshPublicTrackerList()
  const actionMutation = usePublicTrackerAction(instanceId)

  // Check how many torrents are private (will be skipped)
  const privateCount = torrents.filter(t => t.private).length
  const publicCount = torrents.length - privateCount

  const handleExecute = async () => {
    try {
      const result = await actionMutation.mutateAsync({ hashes, pruneMode })

      if (result.processedCount > 0) {
        const added = result.trackersAdded > 0 ? `added ${result.trackersAdded} trackers` : ""
        const removed = result.trackersRemoved > 0 ? `removed ${result.trackersRemoved} dead trackers` : ""
        const actions = [added, removed].filter(Boolean).join(", ")
        toast.success(
          `Updated ${result.processedCount} torrent(s)` + (actions ? `: ${actions}` : "")
        )
      }

      if (result.skippedPrivate > 0) {
        toast.info(`Skipped ${result.skippedPrivate} private torrent(s)`)
      }

      if (result.errors && result.errors.length > 0) {
        toast.warning(`${result.errors.length} error(s) occurred during processing`)
      }

      onOpenChange(false)
    } catch (error) {
      const message = error instanceof Error ? error.message : "Unknown error"
      toast.error(`Failed to execute action: ${message}`)
    }
  }

  const handleRefresh = async () => {
    try {
      await refreshMutation.mutateAsync()
      toast.success("Tracker list refreshed")
    } catch (error) {
      const message = error instanceof Error ? error.message : "Unknown error"
      toast.error(`Failed to refresh tracker list: ${message}`)
    }
  }

  const trackerCount = settings?.cachedTrackers?.length ?? 0

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="!max-w-2xl">
        <AlertDialogHeader>
          <AlertDialogTitle>Manage Public Trackers</AlertDialogTitle>
          <AlertDialogDescription>
            Add reliable public trackers from a curated list to {hashes.length} selected torrent(s).
          </AlertDialogDescription>
        </AlertDialogHeader>

        {privateCount > 0 && (
          <div className="flex items-start gap-2 p-3 bg-yellow-500/10 border border-yellow-500/20 rounded-md">
            <AlertCircle className="h-4 w-4 text-yellow-500 mt-0.5 shrink-0" />
            <div className="text-sm">
              <span className="font-medium text-yellow-600 dark:text-yellow-400">
                {privateCount} private torrent(s) will be skipped
              </span>
              <p className="text-muted-foreground mt-1">
                Private torrents cannot have trackers modified. Only {publicCount} public torrent(s) will be processed.
              </p>
            </div>
          </div>
        )}

        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium">Tracker List</p>
              {settingsLoading ? (
                <p className="text-xs text-muted-foreground">Loading...</p>
              ) : (
                <p className="text-xs text-muted-foreground">
                  {trackerCount} tracker(s) available
                  {settings?.lastFetchedAt && (
                    <span className="ml-2">
                      (last updated: {new Date(settings.lastFetchedAt).toLocaleDateString()})
                    </span>
                  )}
                </p>
              )}
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={handleRefresh}
              disabled={refreshMutation.isPending}
            >
              <RefreshCw className={`h-4 w-4 mr-2 ${refreshMutation.isPending ? "animate-spin" : ""}`} />
              Refresh
            </Button>
          </div>

          <div className="space-y-3">
            <p className="text-sm font-medium">Prune Mode</p>
            <RadioGroup
              value={pruneMode}
              onValueChange={(value) => setPruneMode(value as PruneMode)}
              className="space-y-2"
            >
              <div className="flex items-start space-x-3">
                <RadioGroupItem value="dead" id="prune-dead" />
                <Label htmlFor="prune-dead" className="cursor-pointer">
                  <span className="font-medium">Remove Dead, Add New</span>
                  <p className="text-xs text-muted-foreground">
                    Remove only dead/erroring trackers, keep working ones, add new trackers
                  </p>
                </Label>
              </div>
              <div className="flex items-start space-x-3">
                <RadioGroupItem value="all" id="prune-all" />
                <Label htmlFor="prune-all" className="cursor-pointer">
                  <span className="font-medium">Replace All Trackers</span>
                  <p className="text-xs text-muted-foreground">
                    Remove all existing trackers and replace with the curated list
                  </p>
                </Label>
              </div>
              <div className="flex items-start space-x-3">
                <RadioGroupItem value="none" id="prune-none" />
                <Label htmlFor="prune-none" className="cursor-pointer">
                  <span className="font-medium">Add Missing Trackers</span>
                  <p className="text-xs text-muted-foreground">
                    Keep all existing trackers, only add new ones from the list
                  </p>
                </Label>
              </div>
            </RadioGroup>
          </div>
        </div>

        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleExecute}
            disabled={actionMutation.isPending || publicCount === 0 || trackerCount === 0}
          >
            {actionMutation.isPending ? "Processing..." : "Execute"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
