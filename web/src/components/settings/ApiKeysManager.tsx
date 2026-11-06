/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { CopyButton } from "@/components/ui/copy-button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from "@/components/ui/dialog"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import {
  holdsLevel,
  isAreaRefusedByInstanceLimit,
  PERMISSION_AREA_LABELS,
  PERMISSION_AREAS,
  PERMISSION_LEVEL_LABELS,
  PERMISSION_LEVELS,
  permissionsToSave,
  togglePermission,
  type ApiKeyPermission
} from "@/lib/api-key-permissions"
import { cn } from "@/lib/utils"
import type { ApiKey, ApiKeyRequest, ApplicationInfo, InstanceResponse } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Pencil, Plus, Trash2 } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

function getApiErrorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback
}

function usePermissionText() {
  const { t } = useTranslation("settings")
  return ({ area, level }: ApiKeyPermission) => t("apiKeys.permissions.entry", {
    area: t(PERMISSION_AREA_LABELS[area].labelKey),
    level: t(PERMISSION_LEVEL_LABELS[level].labelKey),
  })
}

interface ApiKeyFormProps {
  initial?: ApiKey
  instances?: InstanceResponse[]
  isPending: boolean
  submitLabel: string
  pendingLabel: string
  onSubmit: (data: ApiKeyRequest) => void
}

function ApiKeyForm({ initial, instances, isPending, submitLabel, pendingLabel, onSubmit }: ApiKeyFormProps) {
  const { t } = useTranslation("settings")
  const permissionText = usePermissionText()
  const [name, setName] = useState(initial?.name ?? "")
  const [nameTouched, setNameTouched] = useState(false)
  const [permissions, setPermissions] = useState<readonly ApiKeyPermission[]>(initial?.permissions ?? [])
  const [instanceIds, setInstanceIds] = useState<readonly number[]>(initial?.instanceIds ?? [])
  const toSave = permissionsToSave(permissions, instanceIds)
  // A deleted instance stays in the list: dropping it can empty the limit, and an empty limit means all instances.
  const deletedIds = instances
    ? [...new Set([...(initial?.instanceIds ?? []), ...instanceIds])].filter(id => !instances.some(instance => instance.id === id))
    : []
  const instanceRows = [
    ...(instances ?? []).map(instance => ({ id: instance.id, label: instance.name })),
    ...deletedIds.map(id => ({ id, label: t("apiKeys.instanceLimit.deleted", { id }) })),
  ]

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        onSubmit({ name, permissions: toSave, instanceIds: [...instanceIds] })
      }}
      className="flex min-h-0 flex-1 flex-col gap-4"
    >
      <div className="-mx-1 min-h-0 flex-1 space-y-4 overflow-y-auto p-1">
        <div className="space-y-2">
          <Label htmlFor="api-key-name">{t("apiKeys.createDialog.nameLabel")}</Label>
          <Input
            id="api-key-name"
            placeholder={t("apiKeys.createDialog.namePlaceholder")}
            value={name}
            onBlur={() => setNameTouched(true)}
            onChange={(e) => setName(e.target.value)}
            data-1p-ignore
            autoComplete="off"
          />
          {nameTouched && !name && (
            <p className="text-sm text-destructive">{t("apiKeys.createDialog.nameRequired")}</p>
          )}
        </div>

        <div className="space-y-2">
          <Label>{t("apiKeys.instanceLimit.title")}</Label>
          <p className="text-sm text-muted-foreground">{t("apiKeys.instanceLimit.help")}</p>
          <div className="grid gap-2 sm:grid-cols-2">
            {instanceRows.map(({ id, label }) => (
              <label key={id} className="flex min-w-0 items-center gap-2 text-sm">
                <Checkbox
                  checked={instanceIds.includes(id)}
                  onCheckedChange={() => setInstanceIds(
                    instanceIds.includes(id) ? instanceIds.filter(other => other !== id) : [...instanceIds, id]
                  )}
                />
                <span className="truncate">{label}</span>
              </label>
            ))}
          </div>
        </div>

        <div className="space-y-2">
          <div className="flex items-center gap-1.5">
            <Label>{t("apiKeys.permissions.title")}</Label>
            <FieldHelp>{t("apiKeys.permissions.help")}</FieldHelp>
          </div>
          {instanceIds.length > 0 && (
            <p className="text-sm text-muted-foreground">{t("apiKeys.permissions.refusedByLimit")}</p>
          )}
          <div className="grid grid-cols-[minmax(0,1fr)_repeat(3,3.5rem)] items-center gap-x-2 gap-y-2 text-sm">
            <span />
            {PERMISSION_LEVELS.map((level) => (
              <span key={level} className="text-center text-xs capitalize text-muted-foreground">{t(PERMISSION_LEVEL_LABELS[level].labelKey)}</span>
            ))}
            {PERMISSION_AREAS.map(({ area, levels }) => {
              const refused = isAreaRefusedByInstanceLimit(area, instanceIds)
              return [
                <span key={area} className={cn("break-words", refused && "text-muted-foreground opacity-50")}>
                  {t(PERMISSION_AREA_LABELS[area].labelKey)}
                </span>,
                ...PERMISSION_LEVELS.map((level) => {
                  if (!levels.includes(level)) {
                    return <span key={`${area}-${level}`} />
                  }
                  const includedByWrite = level !== "write" && holdsLevel(permissions, area, "write")
                  return (
                    <Checkbox
                      key={`${area}-${level}`}
                      aria-label={permissionText({ area, level })}
                      className="justify-self-center"
                      checked={!refused && (includedByWrite || holdsLevel(permissions, area, level))}
                      disabled={refused || includedByWrite}
                      onCheckedChange={() => setPermissions(togglePermission(permissions, area, level))}
                    />
                  )
                }),
              ]
            })}
          </div>
        </div>
      </div>

      {toSave.length === 0 && (
        <p className="text-sm text-muted-foreground">{t("apiKeys.permissions.required")}</p>
      )}
      <Button type="submit" disabled={!name || toSave.length === 0 || isPending} className="w-full flex-shrink-0">
        {isPending ? pendingLabel : submitLabel}
      </Button>
    </form>
  )
}

interface ApiKeysManagerProps {
  authMode?: ApplicationInfo["authMode"]
  authModeLoading: boolean
}

export function ApiKeysManager({ authMode, authModeLoading }: ApiKeysManagerProps) {
  const { t } = useTranslation("settings")
  const permissionText = usePermissionText()
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [editingKey, setEditingKey] = useState<ApiKey | null>(null)
  const [deleteKeyId, setDeleteKeyId] = useState<number | null>(null)
  const [newKey, setNewKey] = useState<{ name: string; key: string } | null>(null)
  const queryClient = useQueryClient()
  const { formatDate } = useDateTimeFormatters()
  const { instances } = useInstances()
  const authDisabled = authMode === "disabled"

  const { data: apiKeys, isLoading } = useQuery({
    queryKey: ["apiKeys"],
    queryFn: () => api.getApiKeys(),
    enabled: !authModeLoading && !authDisabled,
    staleTime: 30 * 1000,
  })

  const keys = apiKeys || []

  const instanceLimitText = (key: ApiKey) => {
    if (key.instanceIds.length === 0) {
      return t("apiKeys.instanceLimit.all")
    }
    if (!instances) {
      return null
    }
    const names = instances.filter(instance => key.instanceIds.includes(instance.id)).map(instance => instance.name)
    return names.length > 0 ? names.join(", ") : t("apiKeys.instanceLimit.none")
  }

  const createMutation = useMutation({
    mutationFn: (data: ApiKeyRequest) => api.createApiKey(data),
    onSuccess: (data) => {
      setNewKey(data)
      queryClient.invalidateQueries({ queryKey: ["apiKeys"] })
      toast.success(t("apiKeys.toasts.created"))
    },
    onError: (error) => {
      toast.error(getApiErrorMessage(error, t("apiKeys.toasts.createFailed")))
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: ApiKeyRequest }) => api.updateApiKey(id, data),
    onSuccess: (_key, { id }) => {
      queryClient.invalidateQueries({ queryKey: ["apiKeys"] })
      setEditingKey(current => current?.id === id ? null : current)
      toast.success(t("apiKeys.toasts.updated"))
    },
    onError: (error) => {
      toast.error(getApiErrorMessage(error, t("apiKeys.toasts.updateFailed")))
    },
  })

  const deleteMutation = useMutation({
    mutationFn: async (id: number) => {
      return api.deleteApiKey(id)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["apiKeys"] })
      setDeleteKeyId(null)
      toast.success(t("apiKeys.toasts.deleted"))
    },
    onError: (error) => {
      toast.error(getApiErrorMessage(error, t("apiKeys.toasts.deleteFailed")))
    },
  })

  if (authModeLoading) {
    return (
      <div className="rounded-md border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
        {t("apiKeys.loadingAuthMode")}
      </div>
    )
  }

  if (authDisabled) {
    return (
      <div className="rounded-md border border-border bg-muted/30 p-4">
        <h3 className="text-sm font-medium">{t("apiKeys.authDisabledTitle")}</h3>
        <p className="mt-2 text-sm text-muted-foreground">
          {t("apiKeys.authDisabledDescription")}
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">
          {t("apiKeys.description")}
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
              {t("apiKeys.createButton")}
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-lg max-h-[90dvh] flex flex-col">
            <DialogHeader className="flex-shrink-0">
              <DialogTitle>{t("apiKeys.createDialog.title")}</DialogTitle>
              <DialogDescription>
                {t("apiKeys.createDialog.description")}
              </DialogDescription>
            </DialogHeader>

            {newKey ? (
              <div className="space-y-4 overflow-y-auto">
                <div>
                  <Label>{t("apiKeys.newKey.label")}</Label>
                  <div className="mt-2 flex items-center gap-2">
                    <code className="flex-1 rounded bg-muted px-2 py-1 text-sm font-mono break-all">
                      {newKey.key}
                    </code>
                    <CopyButton value={newKey.key} label={t("apiKeys.newKey.apiKey")} iconClassName="h-4 w-4" />
                  </div>
                  <p className="mt-2 text-sm text-destructive">
                    {t("apiKeys.newKey.warning")}
                  </p>
                </div>
                <Button
                  onClick={() => {
                    setNewKey(null)
                    setShowCreateDialog(false)
                  }}
                  className="w-full"
                >
                  {t("apiKeys.newKey.done")}
                </Button>
              </div>
            ) : (
              <ApiKeyForm
                instances={instances}
                isPending={createMutation.isPending}
                submitLabel={t("apiKeys.createDialog.submit")}
                pendingLabel={t("apiKeys.createDialog.creating")}
                onSubmit={(data) => createMutation.mutate(data)}
              />
            )}
          </DialogContent>
        </Dialog>
      </div>

      <div className="space-y-2">
        {isLoading ? (
          <p className="text-center text-sm text-muted-foreground py-8">
            {t("apiKeys.loading")}
          </p>
        ) : (
          <>
            {keys.map((key) => {
              const limitText = instanceLimitText(key)
              return (
                <div
                  key={key.id}
                  data-testid="api-key-row"
                  className="flex items-center bg-muted/40 justify-between gap-2 rounded-lg border p-4"
                >
                  <div className="min-w-0 space-y-2">
                    <div className="flex items-center gap-2">
                      <span className="font-medium">{key.name}</span>
                      <Badge variant="outline" className="text-xs">
                        {t("clientApiKeys.idLabel", { id: key.id })}
                      </Badge>
                    </div>
                    <p className="text-sm text-muted-foreground">
                      {t("clientApiKeys.labels.created")} {formatDate(new Date(key.createdAt))}
                      {key.lastUsedAt && (
                        <> • {t("clientApiKeys.labels.lastUsed")} {formatDate(new Date(key.lastUsedAt))}</>
                      )}
                    </p>
                    <div className="flex flex-wrap gap-1">
                      {key.permissions.map((permission) => (
                        <Badge key={`${permission.area}-${permission.level}`} variant="secondary" className="text-xs font-normal">
                          {permissionText(permission)}
                        </Badge>
                      ))}
                    </div>
                    {limitText && (
                      <p className="text-sm text-muted-foreground">
                        {t("apiKeys.instanceLimit.summary", { instances: limitText })}
                      </p>
                    )}
                  </div>
                  <div className="flex flex-shrink-0 items-center gap-1">
                    <Button
                      size="icon"
                      variant="ghost"
                      aria-label={t("apiKeys.editButton", { name: key.name })}
                      onClick={() => setEditingKey(key)}
                    >
                      <Pencil className="h-4 w-4" />
                    </Button>
                    <Button
                      size="icon"
                      variant="ghost"
                      aria-label={t("apiKeys.deleteButton", { name: key.name })}
                      onClick={() => setDeleteKeyId(key.id)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              )
            })}

            {keys.length === 0 && (
              <p className="text-center text-sm text-muted-foreground py-8">
                {t("apiKeys.empty")}
              </p>
            )}
          </>
        )}
      </div>

      <Dialog open={editingKey !== null} onOpenChange={(open) => !open && setEditingKey(null)}>
        <DialogContent className="sm:max-w-lg max-h-[90dvh] flex flex-col">
          <DialogHeader className="flex-shrink-0">
            <DialogTitle>{t("apiKeys.editDialog.title")}</DialogTitle>
            <DialogDescription>
              {t("apiKeys.editDialog.description")}
            </DialogDescription>
          </DialogHeader>
          {editingKey && (
            <ApiKeyForm
              initial={editingKey}
              instances={instances}
              isPending={updateMutation.isPending}
              submitLabel={t("apiKeys.editDialog.submit")}
              pendingLabel={t("apiKeys.editDialog.saving")}
              onSubmit={(data) => updateMutation.mutate({ id: editingKey.id, data })}
            />
          )}
        </DialogContent>
      </Dialog>

      <AlertDialog open={!!deleteKeyId} onOpenChange={() => setDeleteKeyId(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("apiKeys.deleteDialog.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("apiKeys.deleteDialog.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("apiKeys.deleteDialog.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => deleteKeyId && deleteMutation.mutate(deleteKeyId)}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t("apiKeys.deleteDialog.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
