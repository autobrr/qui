/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { InstanceCard } from "@/components/instances/InstanceCard"
import { InstanceForm } from "@/components/instances/InstanceForm"
import { PasswordIssuesBanner } from "@/components/instances/PasswordIssuesBanner"
import { ClientApiKeysManager } from "@/components/settings/ClientApiKeysManager"
import { DateTimePreferencesForm } from "@/components/settings/DateTimePreferencesForm"
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
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import { withBasePath } from "@/lib/base-url"
import { copyTextToClipboard } from "@/lib/utils"
import type { Instance } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { Clock, Copy, ExternalLink, Key, Palette, Plus, Server, Share2, Shield, Trash2 } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

const settingsTabs = ["instances", "client-api", "api", "datetime", "themes", "security"] as const
type SettingsTab = (typeof settingsTabs)[number]

const isSettingsTab = (value: unknown): value is SettingsTab => {
  return typeof value === "string" && settingsTabs.some((tab) => tab === value)
}

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
        <Dialog open={showCreateDialog} onOpenChange={setShowCreateDialog}>
          <DialogTrigger asChild>
            <Button size="sm">
              <Plus className="mr-2 h-4 w-4" />
              Create API Key
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create API Key</DialogTitle>
              <DialogDescription>
                Give your API key a descriptive name to remember its purpose.
              </DialogDescription>
            </DialogHeader>

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
                        autoComplete='off'
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
                className="flex items-center justify-between rounded-lg border p-4"
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

function InstancesManager() {
  const { instances, isLoading, reorderInstances, isReordering } = useInstances()
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as Record<string, unknown> | undefined
  const tab = isSettingsTab(search?.tab) ? search?.tab : undefined
  const modal = typeof search?.modal === "string" ? search?.modal : undefined
  const isDialogOpen = tab === "instances" && modal === "add-instance"
  const [editingInstance, setEditingInstance] = useState<Instance | undefined>()

  const handleOpenDialog = (instance?: Instance) => {
    setEditingInstance(instance)
    const nextSearch: Record<string, unknown> = {
      ...(search ?? {}),
      tab: "instances",
      modal: "add-instance",
    }
    navigate({ search: nextSearch as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
  }

  const handleCloseDialog = () => {
    setEditingInstance(undefined)
    const nextSearch: Record<string, unknown> = {
      ...(search ?? {}),
      tab: "instances",
    }
    delete nextSearch.modal
    navigate({ search: nextSearch as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
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
        <Button onClick={() => handleOpenDialog()} size="sm" className="w-full sm:w-auto">
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
                    onEdit={() => handleOpenDialog(instance)}
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
                  onClick={() => handleOpenDialog()}
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

      <Dialog open={isDialogOpen} onOpenChange={(open) => open ? handleOpenDialog() : handleCloseDialog()}>
        <DialogContent className="sm:max-w-[425px]">
          <DialogHeader>
            <DialogTitle>
              {editingInstance ? "Edit Instance" : "Add Instance"}
            </DialogTitle>
            <DialogDescription>
              {editingInstance? "Update your qBittorrent instance configuration": "Add a new qBittorrent instance to manage"}
            </DialogDescription>
          </DialogHeader>
          <InstanceForm
            instance={editingInstance}
            onSuccess={handleCloseDialog}
            onCancel={handleCloseDialog}
          />
        </DialogContent>
      </Dialog>
    </div>
  )
}

export function Settings() {
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as Record<string, unknown> | undefined
  const tabFromSearch = isSettingsTab(search?.tab) ? search?.tab : undefined
  const activeTab: SettingsTab = tabFromSearch ?? "instances"

  const handleTabChange = (tab: SettingsTab) => {
    const nextSearch: Record<string, unknown> = {
      ...(search ?? {}),
      tab,
    }
    if (tab !== "instances") {
      delete nextSearch.modal
    }
    navigate({ search: nextSearch as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
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
          onValueChange={(value) => {
            if (isSettingsTab(value)) {
              handleTabChange(value)
            }
          }}
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
                activeTab === "instances"? "bg-accent text-accent-foreground": "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Server className="w-4 h-4 mr-2" />
              Instances
            </button>
            <button
              onClick={() => handleTabChange("client-api")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "client-api"? "bg-accent text-accent-foreground": "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Share2 className="w-4 h-4 mr-2" />
              Client Proxy
            </button>
            <button
              onClick={() => handleTabChange("api")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "api"? "bg-accent text-accent-foreground": "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Key className="w-4 h-4 mr-2" />
              API Keys
            </button>
            <button
              onClick={() => handleTabChange("datetime")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "datetime"? "bg-accent text-accent-foreground": "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Clock className="w-4 h-4 mr-2" />
              Date & Time
            </button>
            <button
              onClick={() => handleTabChange("themes")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "themes"? "bg-accent text-accent-foreground": "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Palette className="w-4 h-4 mr-2" />
              Premium Themes
            </button>
            <button
              onClick={() => handleTabChange("security")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "security"? "bg-accent text-accent-foreground": "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Shield className="w-4 h-4 mr-2" />
              Security
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
                  <InstancesManager />
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
              <LicenseManager />
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
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
