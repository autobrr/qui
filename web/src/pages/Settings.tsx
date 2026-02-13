/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { IndexersPage } from "@/components/indexers/IndexersPage"
import { InstanceCard } from "@/components/instances/InstanceCard"
import { InstanceForm } from "@/components/instances/InstanceForm"
import { InstancePreferencesDialog } from "@/components/instances/preferences/InstancePreferencesDialog"
import { PasswordIssuesBanner } from "@/components/instances/PasswordIssuesBanner"
import { ArrInstancesManager } from "@/components/settings/ArrInstancesManager"
import { ClientApiKeysManager } from "@/components/settings/ClientApiKeysManager"
import { DateTimePreferencesForm } from "@/components/settings/DateTimePreferencesForm"
import { ExternalProgramsManager } from "@/components/settings/ExternalProgramsManager"
import { LogSettingsPanel } from "@/components/settings/LogSettingsPanel"
import { NotificationsManager } from "@/components/settings/NotificationsManager"
import { LicenseManager } from "@/components/themes/LicenseManager.tsx"
import { ThemeSelector } from "@/components/themes/ThemeSelector"
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
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstances } from "@/hooks/useInstances"
import { usePersistedTitleBarSpeeds } from "@/hooks/usePersistedTitleBarSpeeds"
import { api } from "@/lib/api"

import { withBasePath } from "@/lib/base-url"
import { canRegisterProtocolHandler, getMagnetHandlerRegistrationGuidance, registerMagnetHandler } from "@/lib/protocol-handler"
import { copyTextToClipboard, formatBytes } from "@/lib/utils"
import type { SettingsSearch } from "@/routes/_authenticated/settings"
import type { Instance, TorznabSearchCacheStats } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Bell, Clock, Copy, Database, ExternalLink, FileText, Key, Layers, Link2, Loader2, Palette, Plus, RefreshCw, Server, Share2, Shield, Terminal, Trash2 } from "lucide-react"
import type { FormEvent } from "react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

type SettingsTab = NonNullable<SettingsSearch["tab"]>

const TORZNAB_CACHE_MIN_TTL_MINUTES = 1440

function ChangePasswordForm() {
  const mutation = useMutation({
    mutationFn: async (data: { currentPassword: string; newPassword: string }) => {
      return api.changePassword(data.currentPassword, data.newPassword)
    },
    onSuccess: () => {
      toast.success("Password changed successfully")
      form.reset()
    },
    onError: () => {
      toast.error("Failed to change password. Please check your current password.")
    },
  })

  const form = useForm({
    defaultValues: {
      currentPassword: "",
      newPassword: "",
      confirmPassword: "",
    },
    onSubmit: async ({ value }) => {
      await mutation.mutateAsync({
        currentPassword: value.currentPassword,
        newPassword: value.newPassword,
      })
    },
  })

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
      className="space-y-4"
    >
      <form.Field
        name="currentPassword"
        validators={{
          onChange: ({ value }) => !value ? "Current password is required" : undefined,
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor="currentPassword">Current Password</Label>
            <Input
              id="currentPassword"
              type="password"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Field
        name="newPassword"
        validators={{
          onChange: ({ value }) => {
            if (!value) return "New password is required"
            if (value.length < 8) return "Password must be at least 8 characters"
            return undefined
          },
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor="newPassword">New Password</Label>
            <Input
              id="newPassword"
              type="password"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Field
        name="confirmPassword"
        validators={{
          onChange: ({ value, fieldApi }) => {
            const newPassword = fieldApi.form.getFieldValue("newPassword")
            if (!value) return "Please confirm your password"
            if (value !== newPassword) return "Passwords do not match"
            return undefined
          },
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor="confirmPassword">Confirm New Password</Label>
            <Input
              id="confirmPassword"
              type="password"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Subscribe
        selector={(state) => [state.canSubmit, state.isSubmitting]}
      >
        {([canSubmit, isSubmitting]) => (
          <Button
            type="submit"
            disabled={!canSubmit || isSubmitting || mutation.isPending}
          >
            {isSubmitting || mutation.isPending ? "Changing..." : "Change Password"}
          </Button>
        )}
      </form.Subscribe>
    </form>
  )
}

function ApiKeysManager() {
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [deleteKeyId, setDeleteKeyId] = useState<number | null>(null)
  const [newKey, setNewKey] = useState<{ name: string; key: string } | null>(null)
  const queryClient = useQueryClient()
  const { formatDate } = useDateTimeFormatters()

  // Fetch API keys from backend
  const { data: apiKeys, isLoading } = useQuery({
    queryKey: ["apiKeys"],
    queryFn: () => api.getApiKeys(),
    staleTime: 30 * 1000, // 30 seconds
  })

  // Ensure apiKeys is always an array
  const keys = apiKeys || []

  const createMutation = useMutation({
    mutationFn: async (name: string) => {
      return api.createApiKey(name)
    },
    onSuccess: (data) => {
      setNewKey(data)
      queryClient.invalidateQueries({ queryKey: ["apiKeys"] })
      toast.success("API key created successfully")
    },
    onError: () => {
      toast.error("Failed to create API key")
    },
  })

  const deleteMutation = useMutation({
    mutationFn: async (id: number) => {
      return api.deleteApiKey(id)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["apiKeys"] })
      setDeleteKeyId(null)
      toast.success("API key deleted successfully")
    },
    onError: () => {
      toast.error("Failed to delete API key")
    },
  })

  const form = useForm({
    defaultValues: {
      name: "",
    },
    onSubmit: async ({ value }) => {
      await createMutation.mutateAsync(value.name)
      form.reset()
    },
  })

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">
          API keys allow external applications to access your qBittorrent instances.
        </p>
        <Dialog
          open={showCreateDialog}
          onOpenChange={(open) => {
            setShowCreateDialog(open)
            if (!open) {
              setNewKey(null)
            }
          }}
        >
          <DialogTrigger asChild>
            <Button size="sm">
              <Plus className="mr-2 h-4 w-4" />
              Create API Key
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-lg max-h-[90dvh] flex flex-col">
            <DialogHeader className="flex-shrink-0">
              <DialogTitle>Create API Key</DialogTitle>
              <DialogDescription>
                Give your API key a descriptive name to remember its purpose.
              </DialogDescription>
            </DialogHeader>

            <div className="flex-1 overflow-y-auto min-h-0">
              {newKey ? (
                <div className="space-y-4">
                  <div>
                    <Label>Your new API key</Label>
                    <div className="mt-2 flex items-center gap-2">
                      <code className="flex-1 rounded bg-muted px-2 py-1 text-sm font-mono break-all">
                        {newKey.key}
                      </code>
                      <Button
                        size="icon"
                        variant="outline"
                        onClick={async () => {
                          try {
                            await copyTextToClipboard(newKey.key)
                            toast.success("API key copied to clipboard")
                          } catch {
                            toast.error("Failed to copy to clipboard")
                          }
                        }}
                      >
                        <Copy className="h-4 w-4" />
                      </Button>
                    </div>
                    <p className="mt-2 text-sm text-destructive">
                      Save this key now. You won't be able to see it again.
                    </p>
                  </div>
                  <Button
                    onClick={() => {
                      setNewKey(null)
                      setShowCreateDialog(false)
                    }}
                    className="w-full"
                  >
                    Done
                  </Button>
                </div>
              ) : (
                <form
                  onSubmit={(e) => {
                    e.preventDefault()
                    form.handleSubmit()
                  }}
                  className="space-y-4"
                >
                  <form.Field
                    name="name"
                    validators={{
                      onChange: ({ value }) => !value ? "Name is required" : undefined,
                    }}
                  >
                    {(field) => (
                      <div className="space-y-2">
                        <Label htmlFor="name">Name</Label>
                        <Input
                          id="name"
                          placeholder="e.g., Automation Script"
                          value={field.state.value}
                          onBlur={field.handleBlur}
                          onChange={(e) => field.handleChange(e.target.value)}
                          data-1p-ignore
                          autoComplete="off"
                        />
                        {field.state.meta.isTouched && field.state.meta.errors[0] && (
                          <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                        )}
                      </div>
                    )}
                  </form.Field>

                  <form.Subscribe
                    selector={(state) => [state.canSubmit, state.isSubmitting]}
                  >
                    {([canSubmit, isSubmitting]) => (
                      <Button
                        type="submit"
                        disabled={!canSubmit || isSubmitting || createMutation.isPending}
                        className="w-full"
                      >
                        {isSubmitting || createMutation.isPending ? "Creating..." : "Create API Key"}
                      </Button>
                    )}
                  </form.Subscribe>
                </form>
              )}
            </div>
          </DialogContent>
        </Dialog>
      </div>

      <div className="space-y-2">
        {isLoading ? (
          <p className="text-center text-sm text-muted-foreground py-8">
            Loading API keys...
          </p>
        ) : (
          <>
            {keys.map((key) => (
              <div
                key={key.id}
                className="flex items-center bg-muted/40 justify-between rounded-lg border p-4"
              >
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{key.name}</span>
                    <Badge variant="outline" className="text-xs">
                      ID: {key.id}
                    </Badge>
                  </div>
                  <p className="text-sm text-muted-foreground">
                    Created: {formatDate(new Date(key.createdAt))}
                    {key.lastUsedAt && (
                      <> • Last used: {formatDate(new Date(key.lastUsedAt))}</>
                    )}
                  </p>
                </div>
                <Button
                  size="icon"
                  variant="ghost"
                  onClick={() => setDeleteKeyId(key.id)}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            ))}

            {keys.length === 0 && (
              <p className="text-center text-sm text-muted-foreground py-8">
                No API keys created yet
              </p>
            )}
          </>
        )}
      </div>

      <AlertDialog open={!!deleteKeyId} onOpenChange={() => setDeleteKeyId(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete API Key?</AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. Any applications using this key will lose access.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => deleteKeyId && deleteMutation.mutate(deleteKeyId)}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

interface InstancesManagerProps {
  search: SettingsSearch
  onSearchChange: (search: SettingsSearch) => void
}

const INSTANCE_FORM_ID = "instance-form"

function InstancesManager({ search, onSearchChange }: InstancesManagerProps) {
  const { instances, isLoading, reorderInstances, isReordering, isCreating } = useInstances()
  const [titleBarSpeedsEnabled, setTitleBarSpeedsEnabled] = usePersistedTitleBarSpeeds(false)
  const isDialogOpen = search.tab === "instances" && search.modal === "add-instance"
  const [editingInstanceId, setEditingInstanceId] = useState<number | null>(null)
  const editingInstance = instances?.find(instance => instance.id === editingInstanceId)

  // Close edit dialog if instance was deleted
  useEffect(() => {
    if (editingInstanceId !== null && !editingInstance && !isLoading) {
      setEditingInstanceId(null)
    }
  }, [editingInstanceId, editingInstance, isLoading])

  const handleOpenAddDialog = () => {
    onSearchChange({ ...search, tab: "instances", modal: "add-instance" })
  }

  const handleCloseDialog = () => {
    onSearchChange({ tab: "instances" })
  }

  const handleEditInstance = (instance: Instance) => {
    setEditingInstanceId(instance.id)
  }

  const handleReorder = (instanceId: number, direction: -1 | 1) => {
    if (!instances || isReordering) return

    const currentIndex = instances.findIndex(instance => instance.id === instanceId)
    if (currentIndex === -1) return

    const targetIndex = currentIndex + direction
    if (targetIndex < 0 || targetIndex >= instances.length) return

    const orderedIds = instances.map(instance => instance.id)
    const [moved] = orderedIds.splice(currentIndex, 1)
    orderedIds.splice(targetIndex, 0, moved)

    reorderInstances(orderedIds, {
      onError: (error) => {
        toast.error("Failed to update instance order", {
          description: error instanceof Error ? error.message : undefined,
        })
      },
    })
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-col items-stretch gap-2 sm:flex-row sm:justify-end">
        <Button onClick={handleOpenAddDialog} size="sm" className="w-full sm:w-auto">
          <Plus className="mr-2 h-4 w-4" />
          Add Instance
        </Button>
      </div>

      <PasswordIssuesBanner instances={instances || []} />

      <div className="space-y-2">
        {isLoading ? (
          <p className="text-center text-sm text-muted-foreground py-8">
            Loading instances...
          </p>
        ) : (
          <>
            {instances && instances.length > 0 ? (
              <div className="grid gap-4 lg:grid-cols-2">
                {instances.map((instance, index) => (
                  <InstanceCard
                    key={instance.id}
                    instance={instance}
                    onEdit={() => handleEditInstance(instance)}
                    onMoveUp={index > 0 ? () => handleReorder(instance.id, -1) : undefined}
                    onMoveDown={index < instances.length - 1 ? () => handleReorder(instance.id, 1) : undefined}
                    disableMoveUp={isReordering}
                    disableMoveDown={isReordering}
                  />
                ))}
              </div>
            ) : (
              <div className="rounded-lg border border-dashed p-12 text-center">
                <p className="text-muted-foreground">No instances configured</p>
                <Button
                  onClick={handleOpenAddDialog}
                  className="mt-4"
                  variant="outline"
                >
                  <Plus className="mr-2 h-4 w-4" />
                  Add your first instance
                </Button>
              </div>
            )}
          </>
        )}
      </div>

      <div className="rounded-lg border p-4">
        <div className="flex items-center justify-between gap-4">
          <div className="space-y-1">
            <Label className="text-sm font-medium">Title bar speeds</Label>
            <p className="text-xs text-muted-foreground">
              Show download and upload speeds in the browser title bar.
            </p>
          </div>
          <Switch
            checked={titleBarSpeedsEnabled}
            onCheckedChange={(checked) => setTitleBarSpeedsEnabled(Boolean(checked))}
          />
        </div>
      </div>

      <Dialog open={isDialogOpen} onOpenChange={(open) => open ? handleOpenAddDialog() : handleCloseDialog()}>
        <DialogContent className="sm:max-w-[425px] max-h-[90dvh] flex flex-col">
          <DialogHeader className="flex-shrink-0">
            <DialogTitle>Add Instance</DialogTitle>
            <DialogDescription>
              Add a new qBittorrent instance to manage
            </DialogDescription>
          </DialogHeader>
          <div className="flex-1 overflow-y-auto min-h-0">
            <InstanceForm
              onSuccess={handleCloseDialog}
              onCancel={handleCloseDialog}
              formId={INSTANCE_FORM_ID}
            />
          </div>
          <DialogFooter className="flex-shrink-0">
            <Button type="button" variant="outline" onClick={handleCloseDialog}>
              Cancel
            </Button>
            <Button type="submit" form={INSTANCE_FORM_ID} disabled={isCreating}>
              {isCreating ? "Adding..." : "Add Instance"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit Instance Preferences Dialog */}
      {editingInstanceId && editingInstance && (
        <InstancePreferencesDialog
          open={true}
          onOpenChange={(open) => !open && setEditingInstanceId(null)}
          instanceId={editingInstance.id}
          instanceName={editingInstance.name}
          instance={editingInstance}
        />
      )}
    </div>
  )
}

function TorznabSearchCachePanel() {
  const queryClient = useQueryClient()
  const statsQuery = useQuery({
    queryKey: ["torznab", "search-cache", "stats"],
    queryFn: () => api.getTorznabSearchCacheStats(),
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
  const { formatDate } = useDateTimeFormatters()

  const stats: TorznabSearchCacheStats | undefined = statsQuery.data
  const [ttlInput, setTtlInput] = useState("")

  const formatCacheTimestamp = useCallback((value?: string | null) => {
    if (!value) {
      return "—"
    }
    const parsed = new Date(value)
    if (Number.isNaN(parsed.getTime())) {
      return "—"
    }
    return formatDate(parsed)
  }, [formatDate])

  useEffect(() => {
    if (stats?.ttlMinutes !== undefined) {
      setTtlInput(String(stats.ttlMinutes))
    }
  }, [stats?.ttlMinutes])

  const updateTTLMutation = useMutation({
    mutationFn: async (nextTTL: number) => {
      return api.updateTorznabSearchCacheSettings(nextTTL)
    },
    onSuccess: (updatedStats) => {
      toast.success(`Cache TTL updated to ${updatedStats.ttlMinutes} minutes`)
      setTtlInput(String(updatedStats.ttlMinutes))
      queryClient.setQueryData(["torznab", "search-cache", "stats"], updatedStats)
      queryClient.invalidateQueries({
        queryKey: ["torznab", "search-cache"],
        exact: false,
      })
    },
    onError: (error: unknown) => {
      const message = error instanceof Error ? error.message : "Failed to update cache TTL"
      toast.error(message)
    },
  })

  const handleUpdateTTL = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const parsed = Number(ttlInput)
    if (!Number.isFinite(parsed)) {
      toast.error("Enter a valid number of minutes")
      return
    }
    const normalized = Math.floor(parsed)
    if (normalized < TORZNAB_CACHE_MIN_TTL_MINUTES) {
      toast.error(`Cache TTL must be at least ${TORZNAB_CACHE_MIN_TTL_MINUTES} minutes`)
      return
    }
    updateTTLMutation.mutate(normalized)
  }

  const ttlMinutes = stats?.ttlMinutes ?? 0
  const approxSize = stats?.approxSizeBytes ?? 0

  const cacheStatusText = stats?.enabled ? "Enabled" : "Disabled"

  const rows = useMemo(
    () => [
      { label: "Entries", value: stats?.entries?.toLocaleString() ?? "0" },
      { label: "Hit count", value: stats?.totalHits?.toLocaleString() ?? "0" },
      { label: "Approx. size", value: approxSize > 0 ? formatBytes(approxSize) : "—" },
      { label: "TTL", value: ttlMinutes > 0 ? `${ttlMinutes} minutes` : "—" },
      { label: "Newest entry", value: formatCacheTimestamp(stats?.newestCachedAt) },
      { label: "Last used", value: formatCacheTimestamp(stats?.lastUsedAt) },
    ],
    [approxSize, formatCacheTimestamp, stats?.entries, stats?.lastUsedAt, stats?.newestCachedAt, stats?.totalHits, ttlMinutes]
  )

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <CardTitle>Torznab Search Cache</CardTitle>
            <CardDescription>Reduce repeated searches by reusing recent Torznab responses.</CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant={stats?.enabled ? "default" : "secondary"}>{cacheStatusText}</Badge>
            <Button
              variant="outline"
              size="sm"
              onClick={() => statsQuery.refetch()}
              disabled={statsQuery.isFetching}
            >
              <RefreshCw className={`mr-2 h-4 w-4 ${statsQuery.isFetching ? "animate-spin" : ""}`} />
              Refresh stats
            </Button>
          </div>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          {rows.map(row => (
            <div key={row.label} className="space-y-1 rounded-lg border p-3 bg-muted/40">
              <p className="text-xs uppercase text-muted-foreground">{row.label}</p>
              <p className="text-lg font-semibold">{row.value}</p>
            </div>
          ))}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Configuration</CardTitle>
          <CardDescription>Control how long cached searches remain valid.</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleUpdateTTL} className="space-y-3">
            <div className="space-y-2">
              <Label htmlFor="torznab-cache-ttl">Cache TTL (minutes)</Label>
              <div className="flex flex-col gap-2 sm:flex-row">
                <Input
                  id="torznab-cache-ttl"
                  type="number"
                  min={TORZNAB_CACHE_MIN_TTL_MINUTES}
                  value={ttlInput}
                  onChange={(event) => setTtlInput(event.target.value)}
                  disabled={updateTTLMutation.isPending}
                />
                <Button type="submit" disabled={updateTTLMutation.isPending}>
                  {updateTTLMutation.isPending ? (
                    <>
                      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                      Saving…
                    </>
                  ) : (
                    "Save TTL"
                  )}
                </Button>
              </div>
            </div>
            <p className="text-xs text-muted-foreground">
              Minimum {TORZNAB_CACHE_MIN_TTL_MINUTES} minutes (24 hours). Larger values reduce load on your indexers at the expense of fresher results.
            </p>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}

interface SettingsProps {
  search: SettingsSearch
  onSearchChange: (search: SettingsSearch) => void
}

export function Settings({ search, onSearchChange }: SettingsProps) {
  const activeTab: SettingsTab = search.tab ?? "instances"

  const handleTabChange = (tab: SettingsTab) => {
    onSearchChange({ tab })
  }

  return (
    <div className="container mx-auto p-4 md:p-6">
      <div className="mb-4 md:mb-6">
        <h1 className="text-2xl md:text-3xl font-bold">Settings</h1>
        <p className="text-muted-foreground mt-1 md:mt-2 text-sm md:text-base">
          Manage your application preferences and security
        </p>
      </div>

      {/* Mobile Dropdown Navigation */}
      <div className="md:hidden mb-4">
        <Select
          value={activeTab}
          onValueChange={(value) => handleTabChange(value as SettingsTab)}
        >
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="instances">
              <div className="flex items-center">
                <Server className="w-4 h-4 mr-2" />
                Instances
              </div>
            </SelectItem>
            <SelectItem value="indexers">
              <div className="flex items-center">
                <Database className="w-4 h-4 mr-2" />
                Indexers
              </div>
            </SelectItem>
            <SelectItem value="search-cache">
              <div className="flex items-center">
                <Layers className="w-4 h-4 mr-2" />
                Search Cache
              </div>
            </SelectItem>
            <SelectItem value="integrations">
              <div className="flex items-center">
                <Link2 className="w-4 h-4 mr-2" />
                Integrations
              </div>
            </SelectItem>
            <SelectItem value="client-api">
              <div className="flex items-center">
                <Share2 className="w-4 h-4 mr-2" />
                Client Proxy
              </div>
            </SelectItem>
            <SelectItem value="api">
              <div className="flex items-center">
                <Key className="w-4 h-4 mr-2" />
                API Keys
              </div>
            </SelectItem>
            <SelectItem value="external-programs">
              <div className="flex items-center">
                <Terminal className="w-4 h-4 mr-2" />
                External Programs
              </div>
            </SelectItem>
            <SelectItem value="notifications">
              <div className="flex items-center">
                <Bell className="w-4 h-4 mr-2" />
                Notifications
              </div>
            </SelectItem>
            <SelectItem value="datetime">
              <div className="flex items-center">
                <Clock className="w-4 h-4 mr-2" />
                Date & Time
              </div>
            </SelectItem>
            <SelectItem value="themes">
              <div className="flex items-center">
                <Palette className="w-4 h-4 mr-2" />
                Premium Themes
              </div>
            </SelectItem>
            <SelectItem value="security">
              <div className="flex items-center">
                <Shield className="w-4 h-4 mr-2" />
                Security
              </div>
            </SelectItem>
            <SelectItem value="logs">
              <div className="flex items-center">
                <FileText className="w-4 h-4 mr-2" />
                Logs
              </div>
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className="flex gap-6">
        {/* Desktop Sidebar Navigation */}
        <div className="hidden md:block w-64 shrink-0">
          <nav className="space-y-1">
            <button
              onClick={() => handleTabChange("instances")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "instances" ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Server className="w-4 h-4 mr-2" />
              Instances
            </button>
            <button
              onClick={() => handleTabChange("indexers")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "indexers" ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Database className="w-4 h-4 mr-2" />
              Indexers
            </button>
            <button
              onClick={() => handleTabChange("search-cache")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "search-cache" ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Layers className="w-4 h-4 mr-2" />
              Search Cache
            </button>
            <button
              onClick={() => handleTabChange("integrations")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "integrations" ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Link2 className="w-4 h-4 mr-2" />
              Integrations
            </button>
            <button
              onClick={() => handleTabChange("client-api")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "client-api" ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Share2 className="w-4 h-4 mr-2" />
              Client Proxy
            </button>
            <button
              onClick={() => handleTabChange("api")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "api" ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Key className="w-4 h-4 mr-2" />
              API Keys
            </button>
            <button
              onClick={() => handleTabChange("external-programs")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "external-programs" ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Terminal className="w-4 h-4 mr-2" />
              External Programs
            </button>
            <button
              onClick={() => handleTabChange("notifications")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "notifications" ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Bell className="w-4 h-4 mr-2" />
              Notifications
            </button>
            <button
              onClick={() => handleTabChange("datetime")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "datetime" ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Clock className="w-4 h-4 mr-2" />
              Date & Time
            </button>
            <button
              onClick={() => handleTabChange("themes")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "themes" ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Palette className="w-4 h-4 mr-2" />
              Premium Themes
            </button>
            <button
              onClick={() => handleTabChange("security")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "security" ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Shield className="w-4 h-4 mr-2" />
              Security
            </button>
            <button
              onClick={() => handleTabChange("logs")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "logs" ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <FileText className="w-4 h-4 mr-2" />
              Logs
            </button>
          </nav>
        </div>

        {/* Main Content Area */}
        <div className="flex-1 min-w-0">

          {activeTab === "instances" && (
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle>Instances</CardTitle>
                  <CardDescription>
                    Manage your qBittorrent connection settings
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <InstancesManager search={search} onSearchChange={onSearchChange} />
                </CardContent>
              </Card>
            </div>
          )}

          {activeTab === "indexers" && (
            <div className="space-y-4">
              <IndexersPage withContainer={false} />
            </div>
          )}

          {activeTab === "search-cache" && (
            <div className="space-y-4">
              <TorznabSearchCachePanel />
            </div>
          )}

          {activeTab === "integrations" && (
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle>ARR Integrations</CardTitle>
                  <CardDescription>
                    Configure Sonarr and Radarr instances for enhanced cross-seed searches using external IDs
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <ArrInstancesManager />
                </CardContent>
              </Card>
            </div>
          )}

          {activeTab === "client-api" && (
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle>Client Proxy API Keys</CardTitle>
                  <CardDescription>
                    Manage API keys for external applications to connect to qBittorrent instances through qui
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <ClientApiKeysManager />
                </CardContent>
              </Card>
            </div>
          )}

          {activeTab === "api" && (
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <div className="flex items-start justify-between">
                    <div className="space-y-1.5">
                      <CardTitle>API Keys</CardTitle>
                      <CardDescription>
                        Manage API keys for external access
                      </CardDescription>
                    </div>
                    <a
                      href={withBasePath("api/docs")}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
                      title="View API documentation"
                    >
                      <span className="hidden sm:inline">API Docs</span>
                      <ExternalLink className="h-3.5 w-3.5" />
                    </a>
                  </div>
                </CardHeader>
                <CardContent>
                  <ApiKeysManager />
                </CardContent>
              </Card>
            </div>
          )}

          {activeTab === "external-programs" && (
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle>External Programs</CardTitle>
                  <CardDescription>
                    Configure external programs or scripts that can be executed from the torrent context menu
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <ExternalProgramsManager />
                </CardContent>
              </Card>
            </div>
          )}

          {activeTab === "notifications" && (
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle>Notifications</CardTitle>
                  <CardDescription>
                    Send alerts and status updates via any Shoutrrr-supported service
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <NotificationsManager />
                </CardContent>
              </Card>
            </div>
          )}

          {activeTab === "datetime" && (
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle>Date & Time Preferences</CardTitle>
                  <CardDescription>
                    Configure timezone, date format, and time display preferences
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <DateTimePreferencesForm />
                </CardContent>
              </Card>
            </div>
          )}

          {activeTab === "themes" && (
            <div className="space-y-4">
              <LicenseManager
                checkoutStatus={search.checkout}
                checkoutPaymentStatus={search.status}
                onCheckoutConsumed={() => onSearchChange({ tab: "themes" })}
              />
              <ThemeSelector />
            </div>
          )}

          {activeTab === "security" && (
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle>Change Password</CardTitle>
                  <CardDescription>
                    Update your account password
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <ChangePasswordForm />
                </CardContent>
              </Card>

              {canRegisterProtocolHandler() && (
                <Card>
                  <CardHeader>
                    <CardTitle>Browser Integration</CardTitle>
                    <CardDescription>
                      Configure how your browser handles magnet links
                    </CardDescription>
                  </CardHeader>
                  <CardContent>
                    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                      <p className="text-sm text-muted-foreground">
                        Register qui as your browser's handler for magnet links.
                        This allows you to open magnet links directly in qui.
                      </p>
                      <Button
                        variant="secondary"
                        onClick={() => {
                          const success = registerMagnetHandler()
                          if (success) {
                            toast.success("Magnet handler registration requested", {
                              description: getMagnetHandlerRegistrationGuidance(),
                            })
                          } else {
                            toast.error("Failed to register magnet handler")
                          }
                        }}
                        className="w-fit"
                      >
                        Register as Handler
                      </Button>
                    </div>
                  </CardContent>
                </Card>
              )}
            </div>
          )}

          {activeTab === "logs" && (
            <div className="space-y-4">
              <LogSettingsPanel />
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
