/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Link } from "@tanstack/react-router"
import { ArrowDownToLine, Clock, Download, FileText, RefreshCw, Trash } from "lucide-react"
import type { ChangeEvent } from "react"
import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import {
  useBackupManifest,
  useBackupRuns,
  useBackupSettings,
  useDeleteBackupRun,
  useTriggerBackup,
  useUpdateBackupSettings
} from "@/hooks/useInstanceBackups"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import type { BackupCategorySnapshot, BackupRun, BackupRunKind, BackupRunStatus } from "@/types"

interface InstanceBackupsProps {
  instanceId: number
}

type SettingsFormState = {
  enabled: boolean
  hourlyEnabled: boolean
  dailyEnabled: boolean
  weeklyEnabled: boolean
  monthlyEnabled: boolean
  keepLast: number
  keepHourly: number
  keepDaily: number
  keepWeekly: number
  keepMonthly: number
  includeCategories: boolean
  includeTags: boolean
  customPath?: string | null
}

const runKindLabels: Record<BackupRunKind, string> = {
  manual: "Manual",
  hourly: "Hourly",
  daily: "Daily",
  weekly: "Weekly",
  monthly: "Monthly",
}

const statusVariants: Record<BackupRunStatus, "default" | "secondary" | "destructive" | "outline"> = {
  pending: "outline",
  running: "secondary",
  success: "default",
  failed: "destructive",
  canceled: "outline",
}

export function InstanceBackups({ instanceId }: InstanceBackupsProps) {
  const { instances } = useInstances()
  const instance = instances?.find(i => i.id === instanceId)

  const { data: settings, isLoading: settingsLoading } = useBackupSettings(instanceId)
  const { data: runs, isLoading: runsLoading } = useBackupRuns(instanceId)
  const updateSettings = useUpdateBackupSettings(instanceId)
  const triggerBackup = useTriggerBackup(instanceId)
  const deleteRun = useDeleteBackupRun(instanceId)
  const { formatDate } = useDateTimeFormatters()

  const [formState, setFormState] = useState<SettingsFormState | null>(null)
  const [manifestRunId, setManifestRunId] = useState<number | undefined>()
  const [manifestOpen, setManifestOpen] = useState(false)
  const [manifestSearch, setManifestSearch] = useState("")

  const { data: manifest, isLoading: manifestLoading } = useBackupManifest(instanceId, manifestRunId)

  const manifestCategoryEntries = useMemo(() => {
    if (!manifest?.categories) return [] as Array<[string, BackupCategorySnapshot]>
    const entries = Object.entries(manifest.categories) as Array<[string, BackupCategorySnapshot]>
    entries.sort((a, b) => a[0].localeCompare(b[0], undefined, { sensitivity: "base" }))
    return entries
  }, [manifest])

  const manifestTags = useMemo(() => {
    if (!manifest?.tags) return [] as string[]
    const tagsList = [...manifest.tags]
    tagsList.sort((a, b) => a.localeCompare(b, undefined, { sensitivity: "base" }))
    return tagsList
  }, [manifest])

  const displayedCategoryEntries = useMemo(() => manifestCategoryEntries.slice(0, 12), [manifestCategoryEntries])
  const remainingCategoryCount = manifestCategoryEntries.length - displayedCategoryEntries.length

  const displayedTags = useMemo(() => manifestTags.slice(0, 30), [manifestTags])
  const remainingTagCount = manifestTags.length - displayedTags.length

  useEffect(() => {
    if (!manifestOpen) {
      setManifestSearch("")
    }
  }, [manifestOpen])

  useEffect(() => {
    setManifestSearch("")
  }, [manifestRunId])

  const filteredManifestItems = useMemo(() => {
    if (!manifest) return []
    const query = manifestSearch.trim().toLowerCase()
    if (!query) return manifest.items

    return manifest.items.filter(item => {
      const haystack = [
        item.name,
        item.category ?? "",
        item.tags?.join(", ") ?? "",
        item.hash,
      ]
        .join(" ")
        .toLowerCase()

      return haystack.includes(query)
    })
  }, [manifest, manifestSearch])

  useEffect(() => {
    if (settings) {
      setFormState({
        enabled: settings.enabled,
        hourlyEnabled: settings.hourlyEnabled,
        dailyEnabled: settings.dailyEnabled,
        weeklyEnabled: settings.weeklyEnabled,
        monthlyEnabled: settings.monthlyEnabled,
        keepLast: settings.keepLast,
        keepHourly: settings.keepHourly,
        keepDaily: settings.keepDaily,
        keepWeekly: settings.keepWeekly,
        keepMonthly: settings.keepMonthly,
        includeCategories: settings.includeCategories,
        includeTags: settings.includeTags,
        customPath: settings.customPath ?? "",
      })
    }
  }, [settings])

  const lastRun = useMemo(() => (runs && runs.length > 0 ? runs[0] : undefined), [runs])

  const handleToggle = (key: keyof SettingsFormState) => (checked: boolean) => {
    setFormState(prev => (prev ? { ...prev, [key]: checked } : prev))
  }

  const handleNumberChange = (key: keyof SettingsFormState) => (event: ChangeEvent<HTMLInputElement>) => {
    const value = parseInt(event.target.value, 10)
    setFormState(prev => (prev ? { ...prev, [key]: Number.isNaN(value) ? 0 : value } : prev))
  }

  const handlePathChange = (event: ChangeEvent<HTMLInputElement>) => {
    const value = event.target.value
    setFormState(prev => (prev ? { ...prev, customPath: value === "" ? "" : value } : prev))
  }

  const handleSave = async () => {
    if (!formState) return
    try {
      await updateSettings.mutateAsync({
        ...formState,
        customPath: formState.customPath === "" ? null : formState.customPath,
      })
      toast.success("Backup settings updated")
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to update backup settings"
      toast.error(message)
    }
  }

  const handleTrigger = async (kind: BackupRunKind = "manual") => {
    try {
      await triggerBackup.mutateAsync({ kind, requestedBy: "ui" })
      toast.success("Backup queued")
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to queue backup"
      toast.error(message)
    }
  }

  const handleDelete = async (run: BackupRun) => {
    try {
      await deleteRun.mutateAsync(run.id)
      toast.success("Backup run deleted")
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to delete backup run"
      toast.error(message)
    }
  }

  const openManifest = (runId: number) => {
    setManifestRunId(runId)
    setManifestOpen(true)
  }

  return (
    <div className="space-y-6 p-4 lg:p-6">
      <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Backups</h1>
          <p className="text-sm text-muted-foreground">
            Manage torrent backups for {instance?.name ?? `instance ${instanceId}`}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" asChild>
            <Link to="/instances/$instanceId" params={{ instanceId: instanceId.toString() }}>
              Back to Torrents
            </Link>
          </Button>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Last backup</CardTitle>
            <Clock className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {runsLoading ? (
              <p className="text-sm text-muted-foreground">Loading...</p>
            ) : lastRun ? (
              <div className="space-y-2">
                <Badge variant={statusVariants[lastRun.status]}>{runKindLabels[lastRun.kind]}</Badge>
                <p className="text-sm">
                  {formatDateSafe(lastRun.completedAt ?? lastRun.requestedAt, formatDate)}
                </p>
                <p className="text-xs text-muted-foreground capitalize">Status: {lastRun.status}</p>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">No backups yet</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Queued backups</CardTitle>
            <RefreshCw className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {runsLoading ? (
              <p className="text-sm text-muted-foreground">Loading...</p>
            ) : (
              <p className="text-2xl font-bold">{runs?.filter(run => run.status === "running" || run.status === "pending").length ?? 0}</p>
            )}
            <p className="text-xs text-muted-foreground">Pending or running backups</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Instance</CardTitle>
            <Download className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <p className="text-sm truncate font-semibold">{instance?.name ?? `Instance ${instanceId}`}</p>
            <p className="text-xs text-muted-foreground break-all">{instance?.host}</p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Backup settings</CardTitle>
        </CardHeader>
        <CardContent className="space-y-6">
          {settingsLoading || !formState ? (
            <p className="text-sm text-muted-foreground">Loading settings...</p>
          ) : (
            <div className="space-y-6">
              <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                <SettingToggle
                  label="Enable backups"
                  description="Allow scheduled backups to run for this instance"
                  checked={formState.enabled}
                  onCheckedChange={handleToggle("enabled")}
                />
                <SettingToggle
                  label="Include categories"
                  description="Group torrents inside the archive by their category"
                  checked={formState.includeCategories}
                  onCheckedChange={handleToggle("includeCategories")}
                />
                <SettingToggle
                  label="Include tags"
                  description="Store torrent tags in the manifest"
                  checked={formState.includeTags}
                  onCheckedChange={handleToggle("includeTags")}
                />
              </div>

              <Separator />

              <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
                <ScheduleControl
                  label="Hourly"
                  checked={formState.hourlyEnabled}
                  onCheckedChange={handleToggle("hourlyEnabled")}
                  value={formState.keepHourly}
                  onValueChange={handleNumberChange("keepHourly")}
                  description="Backups each hour"
                />
                <ScheduleControl
                  label="Daily"
                  checked={formState.dailyEnabled}
                  onCheckedChange={handleToggle("dailyEnabled")}
                  value={formState.keepDaily}
                  onValueChange={handleNumberChange("keepDaily")}
                  description="Backups per day"
                />
                <ScheduleControl
                  label="Weekly"
                  checked={formState.weeklyEnabled}
                  onCheckedChange={handleToggle("weeklyEnabled")}
                  value={formState.keepWeekly}
                  onValueChange={handleNumberChange("keepWeekly")}
                  description="Backups per week"
                />
                <ScheduleControl
                  label="Monthly"
                  checked={formState.monthlyEnabled}
                  onCheckedChange={handleToggle("monthlyEnabled")}
                  value={formState.keepMonthly}
                  onValueChange={handleNumberChange("keepMonthly")}
                  description="Backups per month"
                />
              </div>

              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="keep-last">Keep latest backups</Label>
                  <Input
                    id="keep-last"
                    type="number"
                    min={0}
                    value={formState.keepLast}
                    onChange={handleNumberChange("keepLast")}
                  />
                  <p className="text-xs text-muted-foreground">Maximum total backups to retain across all schedules</p>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="custom-path">Custom backup path</Label>
                  <Input
                    id="custom-path"
                    value={formState.customPath ?? ""}
                    onChange={handlePathChange}
                    placeholder="backups/instance-1"
                  />
                  <p className="text-xs text-muted-foreground">Relative to qui data directory. Leave empty to use default.</p>
                </div>
              </div>

              <div className="flex flex-wrap gap-2">
                <Button onClick={() => handleTrigger("manual")} disabled={triggerBackup.isPending}>
                  <ArrowDownToLine className="mr-2 h-4 w-4" /> Run manual backup
                </Button>
                <Button
                  variant="outline"
                  onClick={handleSave}
                  disabled={updateSettings.isPending}
                >
                  Save changes
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>Backup history</CardTitle>
            <Button variant="outline" size="sm" onClick={() => handleTrigger("manual")} disabled={triggerBackup.isPending}>
              <ArrowDownToLine className="mr-2 h-4 w-4" /> Queue backup
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          {runsLoading ? (
            <p className="text-sm text-muted-foreground">Loading backups...</p>
          ) : runs && runs.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Type</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Requested</TableHead>
                  <TableHead>Completed</TableHead>
                  <TableHead className="text-right">Torrents</TableHead>
                  <TableHead className="text-right">Size</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {runs.map(run => (
                  <TableRow key={run.id}>
                    <TableCell className="font-medium">{runKindLabels[run.kind]}</TableCell>
                    <TableCell>
                      <Badge variant={statusVariants[run.status]} className="capitalize">{run.status}</Badge>
                    </TableCell>
                    <TableCell>{formatDateSafe(run.requestedAt, formatDate)}</TableCell>
                    <TableCell>{formatDateSafe(run.completedAt, formatDate)}</TableCell>
                    <TableCell className="text-right">{run.torrentCount}</TableCell>
                    <TableCell className="text-right">{formatBytes(run.totalBytes)}</TableCell>
                    <TableCell className="flex justify-end gap-2">
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => openManifest(run.id)}
                        aria-label="View manifest"
                      >
                        <FileText className="h-4 w-4" />
                      </Button>
                      {run.archivePath ? (
                        <Button
                          variant="ghost"
                          size="icon"
                          asChild
                          aria-label="Download backup"
                        >
                          <a
                            href={api.getBackupDownloadUrl(instanceId, run.id)}
                            rel="noreferrer"
                          >
                            <Download className="h-4 w-4" />
                          </a>
                        </Button>
                      ) : (
                        <Button variant="ghost" size="icon" disabled aria-label="Download unavailable">
                          <Download className="h-4 w-4" />
                        </Button>
                      )}
                      <AlertDialog>
                        <AlertDialogTrigger asChild>
                          <Button variant="ghost" size="icon" aria-label="Delete backup">
                            <Trash className="h-4 w-4" />
                          </Button>
                        </AlertDialogTrigger>
                        <AlertDialogContent>
                          <AlertDialogHeader>
                            <AlertDialogTitle>Delete backup?</AlertDialogTitle>
                            <AlertDialogDescription>
                              This will remove the backup archive and manifest from disk. This action cannot be undone.
                            </AlertDialogDescription>
                          </AlertDialogHeader>
                          <AlertDialogFooter>
                            <AlertDialogCancel>Cancel</AlertDialogCancel>
                            <AlertDialogAction onClick={() => handleDelete(run)}>
                              Delete
                            </AlertDialogAction>
                          </AlertDialogFooter>
                        </AlertDialogContent>
                      </AlertDialog>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-sm text-muted-foreground">No backups have been created yet.</p>
          )}
        </CardContent>
      </Card>

      <Dialog open={manifestOpen} onOpenChange={(open) => {
        setManifestOpen(open)
        if (!open) {
          setManifestRunId(undefined)
        }
      }}>
        <DialogContent className="!w-[96vw] !max-w-7xl !md:w-[90vw] !h-[92vh] md:!h-[80vh] lg:!h-[75vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle>Backup manifest</DialogTitle>
            <DialogDescription>
              {manifestRunId ? `Run #${manifestRunId}` : "Select a backup to view its manifest"}
            </DialogDescription>
          </DialogHeader>
          {manifestLoading ? (
            <p className="text-sm text-muted-foreground">Loading manifest...</p>
          ) : manifest ? (
            <div className="space-y-4 flex-1 flex flex-col min-h-0">
              <div className="space-y-3 text-sm">
                <div className="flex flex-wrap gap-3 text-muted-foreground">
                  <span className="font-medium text-foreground">Torrents: {manifest.torrentCount}</span>
                  {manifestCategoryEntries.length > 0 && (
                    <span>Categories: {manifestCategoryEntries.length}</span>
                  )}
                  {manifestTags.length > 0 && <span>Tags: {manifestTags.length}</span>}
                  <span>Generated {formatDateSafe(manifest.generatedAt, formatDate)}</span>
                </div>
                {displayedCategoryEntries.length > 0 && (
                  <div>
                    <p className="font-medium text-foreground mb-2">Categories</p>
                    <div className="flex flex-wrap gap-2">
                      {displayedCategoryEntries.map(([name, snapshot]) => (
                        <Badge key={name} variant="secondary" title={snapshot?.savePath ?? undefined}>
                          {name}
                        </Badge>
                      ))}
                      {remainingCategoryCount > 0 && (
                        <Badge variant="outline">+{remainingCategoryCount} more</Badge>
                      )}
                    </div>
                  </div>
                )}
                {displayedTags.length > 0 && (
                  <div>
                    <p className="font-medium text-foreground mb-2">Tags</p>
                    <div className="flex flex-wrap gap-2">
                      {displayedTags.map(tag => (
                        <Badge key={tag} variant="outline">{tag}</Badge>
                      ))}
                      {remainingTagCount > 0 && (
                        <Badge variant="outline">+{remainingTagCount} more</Badge>
                      )}
                    </div>
                  </div>
                )}
              </div>
              <div className="flex w-full justify-end">
                <Input
                  value={manifestSearch}
                  onChange={event => setManifestSearch(event.target.value)}
                  placeholder="Search torrents, tags, categories..."
                  className="w-full sm:w-[18rem] md:w-[16rem]"
                  aria-label="Search backup manifest"
                />
              </div>
              <div className="flex-1 overflow-auto pr-1">
                <Table className="min-w-[640px] w-full">
                  <TableHeader className="sticky top-0 z-10 bg-background">
                    <TableRow>
                      <TableHead>Name</TableHead>
                      <TableHead>Category</TableHead>
                      <TableHead>Tags</TableHead>
                      <TableHead className="text-right">Size</TableHead>
                      <TableHead className="text-right">Cached Torrent</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {filteredManifestItems.length > 0 ? (
                      filteredManifestItems.map(item => (
                        <TableRow key={item.hash + item.archivePath}>
                          <TableCell className="font-medium !max-w-md truncate">{item.name}</TableCell>
                          <TableCell>{item.category ?? "—"}</TableCell>
                          <TableCell className="max-w-sm truncate">{item.tags && item.tags.length > 0 ? item.tags.join(", ") : "—"}</TableCell>
                          <TableCell className="text-right">{formatBytes(item.sizeBytes)}</TableCell>
                          <TableCell className="text-right">
                            {item.torrentBlob && manifestRunId ? (
                              <Button variant="ghost" size="icon" asChild>
                                <a
                                  href={api.getBackupTorrentDownloadUrl(instanceId, manifestRunId, item.hash)}
                                  download
                                  aria-label={`Download ${item.name} torrent`}
                                >
                                  <Download className="h-4 w-4" />
                                </a>
                              </Button>
                            ) : (
                              <span className="text-xs text-muted-foreground">—</span>
                            )}
                          </TableCell>
                        </TableRow>
                      ))
                    ) : (
                      <TableRow>
                        <TableCell colSpan={5} className="py-6 text-center text-sm text-muted-foreground">
                          {manifestSearch ? `No torrents match "${manifestSearch}".` : "No torrents found."}
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </div>
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">Manifest unavailable.</p>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}

function SettingToggle({
  label,
  description,
  checked,
  onCheckedChange,
}: {
  label: string
  description: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <div className="flex items-start justify-between gap-4 rounded-lg border p-4">
      <div>
        <p className="font-medium leading-none mb-1">{label}</p>
        <p className="text-sm text-muted-foreground">{description}</p>
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  )
}

function ScheduleControl({
  label,
  description,
  checked,
  onCheckedChange,
  value,
  onValueChange,
}: {
  label: string
  description: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  value: number
  onValueChange: (event: ChangeEvent<HTMLInputElement>) => void
}) {
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-3">
        <Label className="font-medium">{label}</Label>
        <Switch checked={checked} onCheckedChange={onCheckedChange} />
      </div>
      <Input type="number" min={0} value={value} onChange={onValueChange} disabled={!checked} />
      <p className="text-xs text-muted-foreground">{description}</p>
    </div>
  )
}

function formatBytes(bytes: number): string {
  if (!bytes) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const order = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / Math.pow(1024, order)
  return `${value.toFixed(value >= 10 || order === 0 ? 0 : 1)} ${units[order]}`
}

function formatDateSafe(value: string | null | undefined, formatter: (date: Date) => string): string {
  if (!value) return "—"
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return "—"
  return formatter(parsed)
}
