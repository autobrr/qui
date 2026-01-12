/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import {
  AlertTriangle,
  CheckCircle2,
  Clock,
  FolderSearch,
  Info,
  Loader2,
  Pause,
  Play,
  Plus,
  Settings2,
  Trash2,
  XCircle
} from "lucide-react"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle
} from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { api } from "@/lib/api"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import {
  useCancelDirScan,
  useCreateDirScanDirectory,
  useDeleteDirScanDirectory,
  useDirScanDirectories,
  useDirScanRuns,
  useDirScanSettings,
  useDirScanStatus,
  useTriggerDirScan,
  useUpdateDirScanDirectory,
  useUpdateDirScanSettings
} from "@/hooks/useDirScan"
import type {
  DirScanDirectory,
  DirScanDirectoryCreate,
  DirScanMatchMode,
  DirScanRun,
  DirScanRunStatus,
  Instance
} from "@/types"
import { useQueries } from "@tanstack/react-query"

interface DirScanTabProps {
  instances: Instance[]
}

// Helper to format relative time from a string or Date
function formatRelativeTimeStr(date: string | Date): string {
  return formatRelativeTime(typeof date === "string" ? new Date(date) : date)
}

// Helper to check if a scan run is currently active
function isRunActive(run: DirScanRun): boolean {
  return run.status === "scanning" || run.status === "searching" || run.status === "injecting"
}

export function DirScanTab({ instances }: DirScanTabProps) {
  const { formatISOTimestamp } = useDateTimeFormatters()
  const [selectedDirectoryId, setSelectedDirectoryId] = useState<number | null>(null)
  const [showSettingsDialog, setShowSettingsDialog] = useState(false)
  const [showDirectoryDialog, setShowDirectoryDialog] = useState(false)
  const [editingDirectory, setEditingDirectory] = useState<DirScanDirectory | null>(null)
  const [deleteConfirmId, setDeleteConfirmId] = useState<number | null>(null)

  // Queries
  const { data: settings, isLoading: settingsLoading } = useDirScanSettings()
  const { data: directories = [], isLoading: directoriesLoading } = useDirScanDirectories()
  const updateSettings = useUpdateDirScanSettings()

  // Get status for each directory
  const directoryWithLocalFs = useMemo(
    () => instances.filter((i) => i.hasLocalFilesystemAccess),
    [instances]
  )

  const handleToggleEnabled = useCallback(
    (enabled: boolean) => {
      updateSettings.mutate(
        { enabled },
        {
          onSuccess: () => {
            toast.success(enabled ? "Directory Scanner enabled" : "Directory Scanner disabled")
          },
          onError: (error) => {
            toast.error(`Failed to update settings: ${error.message}`)
          },
        }
      )
    },
    [updateSettings]
  )

  const handleAddDirectory = useCallback(() => {
    setEditingDirectory(null)
    setShowDirectoryDialog(true)
  }, [])

  const handleEditDirectory = useCallback((directory: DirScanDirectory) => {
    setEditingDirectory(directory)
    setShowDirectoryDialog(true)
  }, [])

  if (settingsLoading || directoriesLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="size-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header with Enable Switch */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle className="flex items-center gap-2">
                <FolderSearch className="size-5" />
                Directory Scanner
              </CardTitle>
              <CardDescription>
                Scan local directories for completed downloads and automatically cross-seed them.
              </CardDescription>
            </div>
            <div className="flex items-center gap-4">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowSettingsDialog(true)}
              >
                <Settings2 className="size-4 mr-2" />
                Settings
              </Button>
              <Label htmlFor="dir-scan-enabled" className="flex items-center gap-2">
                <Switch
                  id="dir-scan-enabled"
                  checked={settings?.enabled ?? false}
                  onCheckedChange={handleToggleEnabled}
                  disabled={updateSettings.isPending}
                />
                {settings?.enabled ? "Enabled" : "Disabled"}
              </Label>
            </div>
          </div>
        </CardHeader>
      </Card>

      {/* No Local Access Warning */}
      {directoryWithLocalFs.length === 0 && (
        <Card className="border-yellow-500/50 bg-yellow-500/5">
          <CardContent className="flex items-center gap-3 py-4">
            <AlertTriangle className="size-5 text-yellow-500" />
            <p className="text-sm text-muted-foreground">
              No qBittorrent instances have local filesystem access enabled. Enable it in instance
              settings to use the Directory Scanner.
            </p>
          </CardContent>
        </Card>
      )}

      {/* Directories List */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div>
            <CardTitle>Scan Directories</CardTitle>
            <CardDescription>
              Configure directories to scan for cross-seedable content.
            </CardDescription>
          </div>
          <Button
            onClick={handleAddDirectory}
            disabled={directoryWithLocalFs.length === 0}
          >
            <Plus className="size-4 mr-2" />
            Add Directory
          </Button>
        </CardHeader>
        <CardContent>
          {directories.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-8 text-center text-muted-foreground">
              <FolderSearch className="size-12 mb-4 opacity-50" />
              <p>No directories configured yet.</p>
              <p className="text-sm">Add a directory to start scanning.</p>
            </div>
          ) : (
            <div className="space-y-4">
              {directories.map((directory) => (
                <DirectoryCard
                  key={directory.id}
                  directory={directory}
                  instances={instances}
                  onEdit={handleEditDirectory}
                  onDelete={setDeleteConfirmId}
                  onSelect={setSelectedDirectoryId}
                  isSelected={selectedDirectoryId === directory.id}
                  formatRelativeTime={formatRelativeTimeStr}
                />
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Selected Directory Details */}
      {selectedDirectoryId && (
        <DirectoryDetails
          directoryId={selectedDirectoryId}
          formatDateTime={formatISOTimestamp}
          formatRelativeTime={formatRelativeTimeStr}
        />
      )}

      {/* Settings Dialog */}
      <SettingsDialog
        open={showSettingsDialog}
        onOpenChange={setShowSettingsDialog}
        settings={settings}
        instances={directoryWithLocalFs}
      />

      {/* Directory Dialog */}
      <DirectoryDialog
        open={showDirectoryDialog}
        onOpenChange={setShowDirectoryDialog}
        directory={editingDirectory}
        instances={directoryWithLocalFs}
      />

      {/* Delete Confirmation */}
      <DeleteDirectoryDialog
        directoryId={deleteConfirmId}
        onOpenChange={(open) => !open && setDeleteConfirmId(null)}
      />
    </div>
  )
}

// Directory Card Component
interface DirectoryCardProps {
  directory: DirScanDirectory
  instances: Instance[]
  onEdit: (directory: DirScanDirectory) => void
  onDelete: (id: number) => void
  onSelect: (id: number | null) => void
  isSelected: boolean
  formatRelativeTime: (date: string | Date) => string
}

function DirectoryCard({
  directory,
  instances,
  onEdit,
  onDelete,
  onSelect,
  isSelected,
  formatRelativeTime,
}: DirectoryCardProps) {
  const { data: status } = useDirScanStatus(directory.id)
  const triggerScan = useTriggerDirScan(directory.id)
  const cancelScan = useCancelDirScan(directory.id)

  const targetInstance = useMemo(
    () => instances.find((i) => i.id === directory.targetInstanceId),
    [instances, directory.targetInstanceId]
  )

  const isActive = useMemo(() => {
    if (!status || ("status" in status && status.status === "idle")) return false
    return isRunActive(status as DirScanRun)
  }, [status])

  const handleTrigger = useCallback(() => {
    triggerScan.mutate(undefined, {
      onSuccess: () => toast.success("Scan started"),
      onError: (error) => toast.error(`Failed to start scan: ${error.message}`),
    })
  }, [triggerScan])

  const handleCancel = useCallback(() => {
    cancelScan.mutate(undefined, {
      onSuccess: () => toast.success("Scan canceled"),
      onError: (error) => toast.error(`Failed to cancel scan: ${error.message}`),
    })
  }, [cancelScan])

  return (
    <div
      className={`rounded-lg border p-4 transition-colors cursor-pointer ${
        isSelected ? "border-primary bg-primary/5" : "hover:border-muted-foreground/50"
      }`}
      onClick={() => onSelect(isSelected ? null : directory.id)}
    >
      <div className="grid grid-cols-[1fr_auto] items-start gap-4">
        <div className="min-w-0 space-y-2">
          <div className="flex items-center gap-2">
            <span className="font-mono text-sm truncate">{directory.path}</span>
            {!directory.enabled && (
              <Badge variant="secondary" className="text-xs">
                Disabled
              </Badge>
            )}
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span>Target: {targetInstance?.name ?? "Unknown"}</span>
            <span>Interval: {directory.scanIntervalMinutes}m</span>
            {directory.category && <span>Category: {directory.category}</span>}
            {directory.lastScanAt && (
              <span>Last scan: {formatRelativeTime(directory.lastScanAt)}</span>
            )}
          </div>
          {status && !("status" in status && status.status === "idle") && (
            <DirectoryStatusBadge run={status as DirScanRun} />
          )}
        </div>
        <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
          {isActive ? (
            <Button
              variant="outline"
              size="sm"
              onClick={handleCancel}
              disabled={cancelScan.isPending}
            >
              {cancelScan.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Pause className="size-4" />
              )}
            </Button>
          ) : (
            <Button
              variant="outline"
              size="sm"
              onClick={handleTrigger}
              disabled={triggerScan.isPending || !directory.enabled}
            >
              {triggerScan.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Play className="size-4" />
              )}
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onEdit(directory)}
          >
            <Settings2 className="size-4" />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onDelete(directory.id)}
          >
            <Trash2 className="size-4 text-destructive" />
          </Button>
        </div>
      </div>
    </div>
  )
}

// Status Badge Component
function DirectoryStatusBadge({ run }: { run: DirScanRun }) {
  const statusConfig: Record<DirScanRunStatus, { icon: React.ReactNode; color: string; label: string }> = {
    scanning: { icon: <Loader2 className="size-3 animate-spin" />, color: "text-blue-500", label: "Scanning" },
    searching: { icon: <Loader2 className="size-3 animate-spin" />, color: "text-blue-500", label: "Searching" },
    injecting: { icon: <Loader2 className="size-3 animate-spin" />, color: "text-blue-500", label: "Injecting" },
    success: { icon: <CheckCircle2 className="size-3" />, color: "text-green-500", label: "Success" },
    failed: { icon: <XCircle className="size-3" />, color: "text-red-500", label: "Failed" },
    canceled: { icon: <Clock className="size-3" />, color: "text-yellow-500", label: "Canceled" },
  }

  const config = statusConfig[run.status]
  const hasStats = run.filesFound > 0 || run.matchesFound > 0 || run.torrentsAdded > 0

  return (
    <div className={`flex items-center gap-1.5 text-xs ${config.color}`}>
      {config.icon}
      <span>{config.label}</span>
      {hasStats && (
        <span className="text-muted-foreground">
          ({run.filesFound} files, {run.matchesFound} matches, {run.torrentsAdded} added)
        </span>
      )}
    </div>
  )
}

// Directory Details Component
interface DirectoryDetailsProps {
  directoryId: number
  formatDateTime: (date: string) => string
  formatRelativeTime: (date: string | Date) => string
}

function DirectoryDetails({ directoryId, formatDateTime, formatRelativeTime }: DirectoryDetailsProps) {
  const { data: runs = [], isLoading } = useDirScanRuns(directoryId, { limit: 10 })

  if (isLoading) {
    return (
      <Card>
        <CardContent className="flex items-center justify-center py-8">
          <Loader2 className="size-6 animate-spin text-muted-foreground" />
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Recent Scan Runs</CardTitle>
        <CardDescription>History of recent scans for this directory.</CardDescription>
      </CardHeader>
      <CardContent>
        {runs.length === 0 ? (
          <p className="text-sm text-muted-foreground text-center py-4">
            No scan runs yet.
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Started</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Files</TableHead>
                <TableHead>Matches</TableHead>
                <TableHead>Added</TableHead>
                <TableHead>Duration</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.map((run) => (
                <TableRow key={run.id}>
                  <TableCell>
                    <Tooltip>
                      <TooltipTrigger className="cursor-default">
                        {formatRelativeTime(run.startedAt)}
                      </TooltipTrigger>
                      <TooltipContent>{formatDateTime(run.startedAt)}</TooltipContent>
                    </Tooltip>
                  </TableCell>
                  <TableCell>
                    <DirectoryStatusBadge run={run} />
                  </TableCell>
                  <TableCell>{run.filesFound}</TableCell>
                  <TableCell>{run.matchesFound}</TableCell>
                  <TableCell>{run.torrentsAdded}</TableCell>
                  <TableCell>
                    {run.completedAt ? formatDuration(new Date(run.completedAt).getTime() - new Date(run.startedAt).getTime()) : "-"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

// Settings Dialog
interface SettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  settings: ReturnType<typeof useDirScanSettings>["data"]
  instances: Instance[]
}

function SettingsDialog({ open, onOpenChange, settings, instances }: SettingsDialogProps) {
  const updateSettings = useUpdateDirScanSettings()
  const [form, setForm] = useState({
    matchMode: settings?.matchMode ?? "strict" as DirScanMatchMode,
    sizeTolerancePercent: settings?.sizeTolerancePercent ?? 2,
    minPieceRatio: settings?.minPieceRatio ?? 50,
    allowPartial: settings?.allowPartial ?? false,
    skipPieceBoundarySafetyCheck: settings?.skipPieceBoundarySafetyCheck ?? true,
    startPaused: settings?.startPaused ?? false,
    category: settings?.category ?? "",
    tags: settings?.tags ?? [],
  })

  const instanceIds = useMemo(
    () => Array.from(new Set(instances.map((i) => i.id).filter((id) => id > 0))),
    [instances]
  )

  const metadataQueries = useQueries({
    queries: instanceIds.map((instanceId) => ({
      queryKey: ["instance-metadata", instanceId],
      queryFn: async () => {
        const [categories, tags, preferences] = await Promise.all([
          api.getCategories(instanceId),
          api.getTags(instanceId),
          api.getInstancePreferences(instanceId),
        ])
        return { categories, tags, preferences }
      },
      staleTime: 60_000,
      gcTime: 1_800_000,
      refetchInterval: 30_000,
      refetchIntervalInBackground: false,
      placeholderData: (previousData: unknown) => previousData,
      enabled: open,
    })),
  })

  const aggregatedMetadata = useMemo(() => {
    const categories: Record<string, { name: string; savePath: string }> = {}
    const tags = new Set<string>()

    for (const q of metadataQueries) {
      const data = q.data as undefined | { categories: Record<string, { name: string; savePath: string }>; tags: string[] }
      if (!data) continue
      for (const [name, cat] of Object.entries(data.categories ?? {})) {
        categories[name] = cat
      }
      for (const tag of data.tags ?? []) {
        tags.add(tag)
      }
    }

    return { categories, tags: Array.from(tags) }
  }, [metadataQueries])

  const categorySelectOptions = useMemo(() => {
    const selected = form.category ? [form.category] : []
    return buildCategorySelectOptions(aggregatedMetadata.categories, selected)
  }, [aggregatedMetadata.categories, form.category])

  const tagSelectOptions = useMemo(
    () => buildTagSelectOptions(aggregatedMetadata.tags, form.tags),
    [aggregatedMetadata.tags, form.tags]
  )

  const defaultCategoryPlaceholder = useMemo(() => {
    if (instanceIds.length === 0) {
      return "No qBittorrent instances with local access"
    }
    if (categorySelectOptions.length === 0) {
      return "Type to add a category"
    }
    return "No category"
  }, [instanceIds.length, categorySelectOptions.length])

  const tagPlaceholder = useMemo(() => {
    if (instanceIds.length === 0) {
      return "No qBittorrent instances with local access"
    }
    if (tagSelectOptions.length === 0) {
      return "Type to add tags"
    }
    return "No tags"
  }, [instanceIds.length, tagSelectOptions.length])

  const handleSave = useCallback(() => {
    updateSettings.mutate(form, {
      onSuccess: () => {
        toast.success("Settings saved")
        onOpenChange(false)
      },
      onError: (error) => {
        toast.error(`Failed to save settings: ${error.message}`)
      },
    })
  }, [form, updateSettings, onOpenChange])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Directory Scanner Settings</DialogTitle>
          <DialogDescription>
            Configure global settings for directory scanning.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="match-mode">Match Mode</Label>
            <Select
              value={form.matchMode}
              onValueChange={(value: DirScanMatchMode) =>
                setForm((prev) => ({ ...prev, matchMode: value }))
              }
            >
              <SelectTrigger id="match-mode">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="strict">Strict (name + size)</SelectItem>
                <SelectItem value="flexible">Flexible (size only)</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              Strict mode matches files by name and size. Flexible mode matches by size only.
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="size-tolerance">Size Tolerance (%)</Label>
            <Input
              id="size-tolerance"
              type="number"
              min={0}
              max={10}
              step={0.5}
              value={form.sizeTolerancePercent}
              onChange={(e) =>
                setForm((prev) => ({
                  ...prev,
                  sizeTolerancePercent: parseFloat(e.target.value) || 0,
                }))
              }
            />
            <p className="text-xs text-muted-foreground">
              Allows small size differences when comparing files (useful for minor repacks). Keep low for best accuracy.
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="min-piece-ratio">Minimum Piece Ratio (%)</Label>
            <Input
              id="min-piece-ratio"
              type="number"
              min={0}
              max={100}
              value={form.minPieceRatio}
              onChange={(e) =>
                setForm((prev) => ({
                  ...prev,
                  minPieceRatio: parseFloat(e.target.value) || 0,
                }))
              }
            />
            <p className="text-xs text-muted-foreground">
              Only used for partial matches. Requires at least this % of the torrent’s data to already be on disk.
            </p>
          </div>

          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Switch
                id="allow-partial"
                checked={form.allowPartial}
                onCheckedChange={(checked) =>
                  setForm((prev) => ({ ...prev, allowPartial: checked }))
                }
              />
              <Label htmlFor="allow-partial" className="flex items-center gap-1">
                Allow partial matches
                <Tooltip>
                  <TooltipTrigger>
                    <Info className="size-3.5 text-muted-foreground" />
                  </TooltipTrigger>
                  <TooltipContent className="max-w-xs">
                    Allows adding torrents even when the torrent has extra/missing files compared to what’s on disk. qBittorrent may download missing files into the save path.
                  </TooltipContent>
                </Tooltip>
              </Label>
            </div>
            <p className="text-xs text-muted-foreground">
              Useful for packs/extras; be careful if scanning your *arr library folders.
            </p>
          </div>

          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Switch
                id="skip-piece-boundary"
                checked={form.skipPieceBoundarySafetyCheck}
                onCheckedChange={(checked) =>
                  setForm((prev) => ({ ...prev, skipPieceBoundarySafetyCheck: checked }))
                }
              />
              <Label htmlFor="skip-piece-boundary" className="flex items-center gap-1">
                Skip piece boundary safety check
                <Tooltip>
                  <TooltipTrigger>
                    <Info className="size-3.5 text-muted-foreground" />
                  </TooltipTrigger>
                  <TooltipContent className="max-w-xs">
                    When disabled, qui will block partial matches where downloading missing files could overlap pieces that include your already-present content.
                  </TooltipContent>
                </Tooltip>
              </Label>
            </div>
            <p className="text-xs text-muted-foreground">
              Only relevant for partial matches. Disable (recommended) for extra safety.
            </p>
          </div>

          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Switch
                id="start-paused"
                checked={form.startPaused}
                onCheckedChange={(checked) =>
                  setForm((prev) => ({ ...prev, startPaused: checked }))
                }
              />
              <Label htmlFor="start-paused">Start torrents paused</Label>
            </div>
            <p className="text-xs text-muted-foreground">
              Adds Dir Scan matches in a paused state (useful if you want to review before seeding).
            </p>
          </div>

          <div className="space-y-2">
            <Label>Default Category</Label>
            <MultiSelect
              options={categorySelectOptions}
              selected={form.category ? [form.category] : []}
              onChange={(values) =>
                setForm((prev) => ({ ...prev, category: values.at(-1) ?? "" }))
              }
              placeholder={defaultCategoryPlaceholder}
              creatable
              disabled={updateSettings.isPending}
            />
            <p className="text-xs text-muted-foreground">
              Category for injected torrents when the scan directory doesn’t override it.
            </p>
          </div>

          <div className="space-y-2">
            <Label>Tags</Label>
            <MultiSelect
              options={tagSelectOptions}
              selected={form.tags}
              onChange={(values) =>
                setForm((prev) => ({ ...prev, tags: values }))
              }
              placeholder={tagPlaceholder}
              creatable
              disabled={updateSettings.isPending}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={updateSettings.isPending}>
            {updateSettings.isPending && <Loader2 className="size-4 mr-2 animate-spin" />}
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Directory Dialog
interface DirectoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  directory: DirScanDirectory | null
  instances: Instance[]
}

function DirectoryDialog({ open, onOpenChange, directory, instances }: DirectoryDialogProps) {
  const createDirectory = useCreateDirScanDirectory()
  const updateDirectory = useUpdateDirScanDirectory(directory?.id ?? 0)
  const isEditing = directory !== null

  const defaultTargetInstanceId = instances[0]?.id ?? 0

  const [form, setForm] = useState<DirScanDirectoryCreate>(() => ({
    path: directory?.path ?? "",
    qbitPathPrefix: directory?.qbitPathPrefix ?? "",
    category: directory?.category ?? "",
    tags: directory?.tags ?? [],
    enabled: directory?.enabled ?? true,
    targetInstanceId: directory?.targetInstanceId ?? defaultTargetInstanceId,
    scanIntervalMinutes: directory?.scanIntervalMinutes ?? 1440,
  }))

  const { data: targetInstanceMetadata, isError: targetInstanceMetadataError } = useInstanceMetadata(form.targetInstanceId)

  const directoryCategoryOptions = useMemo(() => {
    const selected = form.category ? [form.category] : []
    return buildCategorySelectOptions(targetInstanceMetadata?.categories ?? {}, selected)
  }, [targetInstanceMetadata?.categories, form.category])

  const directoryTagOptions = useMemo(
    () => buildTagSelectOptions(targetInstanceMetadata?.tags ?? [], form.tags ?? []),
    [targetInstanceMetadata?.tags, form.tags]
  )

  // Reset form when directory or dialog state changes
  useEffect(() => {
    if (!open) return
    if (directory) {
      setForm({
        path: directory.path,
        qbitPathPrefix: directory.qbitPathPrefix ?? "",
        category: directory.category ?? "",
        tags: directory.tags ?? [],
        enabled: directory.enabled,
        targetInstanceId: directory.targetInstanceId,
        scanIntervalMinutes: directory.scanIntervalMinutes,
      })
    } else {
      setForm({
        path: "",
        qbitPathPrefix: "",
        category: "",
        tags: [],
        enabled: true,
        targetInstanceId: defaultTargetInstanceId,
        scanIntervalMinutes: 1440,
      })
    }
  }, [open, directory, defaultTargetInstanceId])

  const handleSave = useCallback(() => {
    if (isEditing) {
      updateDirectory.mutate(form, {
        onSuccess: () => {
          toast.success("Directory updated")
          onOpenChange(false)
        },
        onError: (error) => {
          toast.error(`Failed to update directory: ${error.message}`)
        },
      })
    } else {
      createDirectory.mutate(form, {
        onSuccess: () => {
          toast.success("Directory created")
          onOpenChange(false)
        },
        onError: (error) => {
          toast.error(`Failed to create directory: ${error.message}`)
        },
      })
    }
  }, [isEditing, form, createDirectory, updateDirectory, onOpenChange])

  const isPending = createDirectory.isPending || updateDirectory.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{isEditing ? "Edit Directory" : "Add Directory"}</DialogTitle>
          <DialogDescription>
            {isEditing ? "Update the directory configuration." : "Add a new directory to scan for cross-seedable content."}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="dir-path">Directory Path</Label>
            <Input
              id="dir-path"
              placeholder="/data/downloads/completed"
              value={form.path}
              onChange={(e) => setForm((prev) => ({ ...prev, path: e.target.value }))}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="qbit-path-prefix" className="flex items-center gap-1">
              qBittorrent Path Prefix
              <Tooltip>
                <TooltipTrigger>
                  <Info className="size-3.5 text-muted-foreground" />
                </TooltipTrigger>
                <TooltipContent className="max-w-xs">
                  Optional path mapping for container setups. If qui sees files at /data/downloads
                  but qBittorrent sees them at /downloads, set this to /downloads.
                </TooltipContent>
              </Tooltip>
            </Label>
            <Input
              id="qbit-path-prefix"
              placeholder="Optional: /downloads"
              value={form.qbitPathPrefix}
              onChange={(e) =>
                setForm((prev) => ({ ...prev, qbitPathPrefix: e.target.value }))
              }
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="target-instance">Target qBittorrent Instance</Label>
            <Select
              value={String(form.targetInstanceId)}
              onValueChange={(value) =>
                setForm((prev) => ({ ...prev, targetInstanceId: parseInt(value, 10) }))
              }
            >
              <SelectTrigger id="target-instance">
                <SelectValue placeholder="Select instance" />
              </SelectTrigger>
              <SelectContent>
                {instances.map((instance) => (
                  <SelectItem key={instance.id} value={String(instance.id)}>
                    {instance.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <Label>Category Override</Label>
            <MultiSelect
              options={directoryCategoryOptions}
              selected={form.category ? [form.category] : []}
              onChange={(values) =>
                setForm((prev) => ({ ...prev, category: values.at(-1) ?? "" }))
              }
              placeholder={
                directoryCategoryOptions.length ? "Use global default category" : "Type to add a category"
              }
              creatable
              disabled={isPending}
            />
            {targetInstanceMetadataError && (
              <p className="text-xs text-muted-foreground">
                Could not load categories from qBittorrent. You can still type a custom value.
              </p>
            )}
            <p className="text-xs text-muted-foreground">
              Optional. When set, overrides the global default category for this directory.
            </p>
          </div>

          <div className="space-y-2">
            <Label>Additional Tags</Label>
            <MultiSelect
              options={directoryTagOptions}
              selected={form.tags ?? []}
              onChange={(values) => setForm((prev) => ({ ...prev, tags: values }))}
              placeholder={
                directoryTagOptions.length ? "Add tags (optional)" : "Type to add tags"
              }
              creatable
              disabled={isPending}
            />
            {targetInstanceMetadataError && (
              <p className="text-xs text-muted-foreground">
                Could not load tags from qBittorrent. You can still type custom values.
              </p>
            )}
            <p className="text-xs text-muted-foreground">
              Added on top of the global Dir Scan tags. Suggested: <span className="font-mono">dirscan</span>, <span className="font-mono">needs-review</span>.
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="scan-interval">Scan Interval (minutes)</Label>
            <Input
              id="scan-interval"
              type="number"
              min={60}
              value={form.scanIntervalMinutes}
              onChange={(e) =>
                setForm((prev) => ({
                  ...prev,
                  scanIntervalMinutes: parseInt(e.target.value, 10) || 1440,
                }))
              }
            />
            <p className="text-xs text-muted-foreground">
              Minimum: 60 minutes (1 hour). Default: 1440 minutes (24 hours).
            </p>
          </div>

          <div className="flex items-center gap-2">
            <Switch
              id="dir-enabled"
              checked={form.enabled}
              onCheckedChange={(checked) =>
                setForm((prev) => ({ ...prev, enabled: checked }))
              }
            />
            <Label htmlFor="dir-enabled">Enabled</Label>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={isPending || !form.path || !form.targetInstanceId}>
            {isPending && <Loader2 className="size-4 mr-2 animate-spin" />}
            {isEditing ? "Save" : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Delete Confirmation Dialog
interface DeleteDirectoryDialogProps {
  directoryId: number | null
  onOpenChange: (open: boolean) => void
}

function DeleteDirectoryDialog({ directoryId, onOpenChange }: DeleteDirectoryDialogProps) {
  const deleteDirectory = useDeleteDirScanDirectory()

  const handleDelete = useCallback(() => {
    if (!directoryId) return
    deleteDirectory.mutate(directoryId, {
      onSuccess: () => {
        toast.success("Directory deleted")
        onOpenChange(false)
      },
      onError: (error) => {
        toast.error(`Failed to delete directory: ${error.message}`)
      },
    })
  }, [directoryId, deleteDirectory, onOpenChange])

  return (
    <AlertDialog open={directoryId !== null} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete Directory</AlertDialogTitle>
          <AlertDialogDescription>
            Are you sure you want to delete this directory configuration? This will also remove all
            tracked files and scan history for this directory.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleDelete}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {deleteDirectory.isPending && <Loader2 className="size-4 mr-2 animate-spin" />}
            Delete
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

// Helper function
function formatDuration(ms: number): string {
  if (ms < 1000) return "<1s"
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = seconds % 60
  if (minutes < 60) return `${minutes}m ${remainingSeconds}s`
  const hours = Math.floor(minutes / 60)
  const remainingMinutes = minutes % 60
  return `${hours}h ${remainingMinutes}m`
}
