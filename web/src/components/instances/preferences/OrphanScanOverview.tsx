/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { OrphanScanPreviewDialog } from "@/components/instances/preferences/OrphanScanPreviewDialog"
import { useInstances } from "@/hooks/useInstances"
import {
  useCancelOrphanScanRun,
  useOrphanScanRuns,
  useOrphanScanSettings,
  useTriggerOrphanScan,
  useUpdateOrphanScanSettings,
} from "@/hooks/useOrphanScan"
import { cn, formatBytes, formatRelativeTime } from "@/lib/utils"
import type { Instance, OrphanScanRunStatus } from "@/types"
import { AlertTriangle, Eye, Files, Info, Loader2, Play, Settings2, X } from "lucide-react"
import { useMemo, useState } from "react"
import { toast } from "sonner"

interface OrphanScanOverviewProps {
  onConfigureInstance?: (instanceId: number) => void
}

function getStatusBadge(status: OrphanScanRunStatus) {
  switch (status) {
    case "pending":
    case "scanning":
      return { variant: "outline" as const, className: "bg-blue-500/10 text-blue-500 border-blue-500/20", label: status === "pending" ? "Starting..." : "Scanning..." }
    case "preview_ready":
      return { variant: "outline" as const, className: "bg-yellow-500/10 text-yellow-500 border-yellow-500/20", label: "Ready for Review" }
    case "deleting":
      return { variant: "outline" as const, className: "bg-orange-500/10 text-orange-500 border-orange-500/20", label: "Deleting..." }
    case "completed":
      return { variant: "outline" as const, className: "bg-emerald-500/10 text-emerald-500 border-emerald-500/20", label: "Completed" }
    case "failed":
      return { variant: "outline" as const, className: "bg-destructive/10 text-destructive border-destructive/30", label: "Failed" }
    case "canceled":
      return { variant: "outline" as const, className: "bg-muted text-muted-foreground border-border/60", label: "Canceled" }
    default:
      return { variant: "outline" as const, className: "", label: status }
  }
}

function InstanceOrphanScanItem({
  instance,
  onConfigureInstance,
}: {
  instance: Instance
  onConfigureInstance?: (instanceId: number) => void
}) {
  const hasLocalAccess = instance.hasLocalFilesystemAccess
  const settingsQuery = useOrphanScanSettings(instance.id, { enabled: hasLocalAccess })
  const runsQuery = useOrphanScanRuns(instance.id, { limit: 5, enabled: hasLocalAccess })
  const triggerMutation = useTriggerOrphanScan(instance.id)
  const updateSettingsMutation = useUpdateOrphanScanSettings(instance.id)
  const cancelMutation = useCancelOrphanScanRun(instance.id)
  const [previewOpen, setPreviewOpen] = useState(false)

  const settings = settingsQuery.data
  const runs = runsQuery.data ?? []
  const latestRun = runs[0]

  const isEnabled = settings?.enabled ?? false
  const isActiveRun = latestRun && ["pending", "scanning", "deleting"].includes(latestRun.status)

  const handleToggleEnabled = (enabled: boolean) => {
    updateSettingsMutation.mutate(
      { enabled },
      {
        onSuccess: () => {
          toast.success(enabled ? "Scheduled scanning enabled" : "Scheduled scanning disabled", {
            description: instance.name,
          })
        },
        onError: (error) => {
          toast.error("Update failed", {
            description: error instanceof Error ? error.message : "Unable to update settings",
          })
        },
      }
    )
  }

  const handleTriggerScan = () => {
    triggerMutation.mutate(undefined, {
      onSuccess: () => {
        toast.success("Scan started", { description: instance.name })
      },
      onError: (error) => {
        toast.error("Failed to start scan", {
          description: error instanceof Error ? error.message : "Unknown error",
        })
      },
    })
  }

  const handleCancelRun = (runId: number) => {
    cancelMutation.mutate(runId, {
      onSuccess: () => {
        toast.success("Scan canceled", { description: instance.name })
      },
      onError: (error) => {
        toast.error("Failed to cancel", {
          description: error instanceof Error ? error.message : "Unknown error",
        })
      },
    })
  }

  if (!hasLocalAccess) {
    return (
      <AccordionItem value={String(instance.id)} disabled>
        <div className="px-6 py-4 flex items-center justify-between opacity-60">
          <div className="flex items-center gap-3">
            <span className="font-medium">{instance.name}</span>
            <Badge variant="outline" className="text-xs">No Local Access</Badge>
          </div>
          <Tooltip>
            <TooltipTrigger asChild>
              <AlertTriangle className="h-4 w-4 text-muted-foreground cursor-help" />
            </TooltipTrigger>
            <TooltipContent className="max-w-[250px]">
              <p>qui and qBittorrent must run on the same machine. Enable "Local Filesystem Access" in instance settings to use orphan scanning.</p>
            </TooltipContent>
          </Tooltip>
        </div>
      </AccordionItem>
    )
  }

  return (
    <AccordionItem value={String(instance.id)}>
      <AccordionTrigger className="px-6 py-4 hover:no-underline group">
        <div className="flex items-center justify-between w-full pr-4">
          <div className="flex items-center gap-3 min-w-0">
            <span className="font-medium truncate">{instance.name}</span>
            {latestRun && (
              <Badge {...getStatusBadge(latestRun.status)} className={cn("text-xs", getStatusBadge(latestRun.status).className)}>
                {getStatusBadge(latestRun.status).label}
              </Badge>
            )}
            {latestRun?.status === "preview_ready" && latestRun.filesFound > 0 && (
              <Badge variant="outline" className="text-xs">
                {latestRun.filesFound} files ({formatBytes(latestRun.bytesReclaimed || 0)})
              </Badge>
            )}
          </div>

          <div className="flex items-center gap-4">
            {latestRun?.completedAt && (
              <span className="text-xs text-muted-foreground hidden sm:block">
                {formatRelativeTime(new Date(latestRun.completedAt))}
              </span>
            )}
            <div
              className="flex items-center gap-2"
              onClick={(e) => e.stopPropagation()}
            >
              <span className={cn(
                "text-xs font-medium",
                isEnabled ? "text-emerald-500" : "text-muted-foreground"
              )}>
                {isEnabled ? "On" : "Off"}
              </span>
              <Switch
                checked={isEnabled}
                onCheckedChange={handleToggleEnabled}
                disabled={updateSettingsMutation.isPending}
                className="scale-90"
              />
            </div>
          </div>
        </div>
      </AccordionTrigger>

      <AccordionContent className="px-6 pb-4">
        <div className="space-y-4">
          {/* Settings summary */}
          <div className="flex items-center justify-between p-3 rounded-lg bg-muted/30 border border-border/40">
            <div className="space-y-0.5">
              <p className="text-sm text-muted-foreground">
                {settings
                  ? `Grace ${settings.gracePeriodMinutes}min · Interval ${settings.scanIntervalHours}h · Max ${settings.maxFilesPerRun} files`
                  : "Loading..."}
              </p>
              {settings?.ignorePaths && settings.ignorePaths.length > 0 && (
                <p className="text-xs text-muted-foreground/70">
                  {settings.ignorePaths.length} path{settings.ignorePaths.length !== 1 ? "s" : ""} ignored
                </p>
              )}
            </div>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={handleTriggerScan}
                disabled={isActiveRun || triggerMutation.isPending}
                className="h-8"
              >
                {triggerMutation.isPending ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <>
                    <Play className="h-4 w-4 mr-2" />
                    Scan Now
                  </>
                )}
              </Button>
              {onConfigureInstance && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => onConfigureInstance(instance.id)}
                  className="h-8"
                >
                  <Settings2 className="h-4 w-4 mr-2" />
                  Configure
                </Button>
              )}
            </div>
          </div>

          {/* Preview ready actions */}
          {latestRun?.status === "preview_ready" && latestRun.filesFound > 0 && (
            <div className="p-4 rounded-lg border border-yellow-500/30 bg-yellow-500/5 space-y-3">
              <div className="flex items-start gap-3">
                <Files className="h-5 w-5 text-yellow-500 shrink-0 mt-0.5" />
                <div className="flex-1 min-w-0">
                  <p className="font-medium text-sm">
                    {latestRun.filesFound} orphan file{latestRun.filesFound !== 1 ? "s" : ""} found
                  </p>
                  <p className="text-xs text-muted-foreground">
                    Total size: {formatBytes(latestRun.bytesReclaimed || 0)}
                    {latestRun.truncated && " (scan was truncated, more files may exist)"}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setPreviewOpen(true)}
                  className="h-8"
                >
                  <Eye className="h-4 w-4 mr-2" />
                  View Preview
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => handleCancelRun(latestRun.id)}
                  disabled={cancelMutation.isPending}
                  className="h-8"
                >
                  <X className="h-4 w-4 mr-2" />
                  Cancel
                </Button>
              </div>
            </div>
          )}

          {latestRun?.status === "preview_ready" && latestRun.filesFound > 0 && (
            <OrphanScanPreviewDialog
              open={previewOpen}
              onOpenChange={setPreviewOpen}
              instanceId={instance.id}
              runId={latestRun.id}
            />
          )}

          {/* Recent runs */}
          {runs.length > 0 && (
            <div className="space-y-2">
              <h4 className="text-sm font-medium">Recent Scans</h4>
              <div className="rounded-md border divide-y">
                {runs.map((run) => {
                  const statusBadge = getStatusBadge(run.status)
                  return (
                    <div key={run.id} className="p-3 flex items-center justify-between">
                      <div className="flex items-center gap-3">
                        <Badge {...statusBadge} className={cn("text-xs", statusBadge.className)}>
                          {statusBadge.label}
                        </Badge>
                        <span className="text-xs text-muted-foreground capitalize">{run.triggeredBy}</span>
                      </div>
                      <div className="flex items-center gap-3 text-xs text-muted-foreground">
                        {run.status === "completed" && (
                          <span>
                            {run.filesDeleted} deleted · {formatBytes(run.bytesReclaimed)}
                          </span>
                        )}
                        {run.startedAt && (
                          <span>{formatRelativeTime(new Date(run.startedAt))}</span>
                        )}
                      </div>
                    </div>
                  )
                })}
              </div>
            </div>
          )}

          {runs.length === 0 && !runsQuery.isLoading && (
            <div className="flex flex-col items-center justify-center py-6 text-center space-y-2 border border-dashed rounded-lg">
              <div className="p-2 rounded-full bg-muted/50">
                <Files className="h-5 w-5 text-muted-foreground/50" />
              </div>
              <p className="text-sm text-muted-foreground">
                No scans run yet. Click "Scan Now" to find orphan files.
              </p>
            </div>
          )}
        </div>
      </AccordionContent>
    </AccordionItem>
  )
}

export function OrphanScanOverview({ onConfigureInstance }: OrphanScanOverviewProps) {
  const { instances } = useInstances()
  const [expandedInstances, setExpandedInstances] = useState<string[]>([])

  const activeInstances = useMemo(
    () => (instances ?? []).filter((inst) => inst.isActive),
    [instances]
  )

  if (!instances || instances.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">Orphan File Scanner</CardTitle>
          <CardDescription>
            No instances configured. Add one in Settings to use this service.
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader className="space-y-2">
        <div className="flex items-center gap-2">
          <CardTitle className="text-lg font-semibold">Orphan File Scanner</CardTitle>
          <Tooltip>
            <TooltipTrigger asChild>
              <Info className="h-4 w-4 text-muted-foreground cursor-help" />
            </TooltipTrigger>
            <TooltipContent className="max-w-[300px]">
              <p>
                Finds files on disk that are not associated with any torrent in qBittorrent.
                Requires local filesystem access to be enabled for each instance.
              </p>
            </TooltipContent>
          </Tooltip>
        </div>
        <CardDescription>
          Scans download directories and identifies orphan files for cleanup.
        </CardDescription>
      </CardHeader>

      <CardContent className="p-0">
        <Accordion
          type="multiple"
          value={expandedInstances}
          onValueChange={setExpandedInstances}
          className="border-t"
        >
          {activeInstances.map((instance) => (
            <InstanceOrphanScanItem
              key={instance.id}
              instance={instance}
              onConfigureInstance={onConfigureInstance}
            />
          ))}
        </Accordion>
      </CardContent>
    </Card>
  )
}
