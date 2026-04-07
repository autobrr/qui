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
import { Switch } from "@/components/ui/switch"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { api } from "@/lib/api"
import type {
  ArrInstance,
  ArrInstanceFormData,
  ArrInstanceType,
  ArrInstanceUpdateData
} from "@/types/arr"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { CheckCircle, Edit, Loader2, Plus, Trash2, XCircle, Zap } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

export function ArrInstancesManager() {
  const { t } = useTranslation("settings")
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [editInstance, setEditInstance] = useState<ArrInstance | null>(null)
  const [deleteInstance, setDeleteInstance] = useState<ArrInstance | null>(null)
  const [testingId, setTestingId] = useState<number | null>(null)
  const queryClient = useQueryClient()
  const { formatDate } = useDateTimeFormatters()

  const { data: instances, isLoading, error } = useQuery({
    queryKey: ["arrInstances"],
    queryFn: () => api.listArrInstances(),
    staleTime: 30 * 1000,
  })

  const createMutation = useMutation({
    mutationFn: async (data: ArrInstanceFormData) => {
      return api.createArrInstance(data)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["arrInstances"] })
      setShowCreateDialog(false)
      toast.success(t("arrInstances.toasts.created"))
    },
    onError: (error: Error) => {
      toast.error(t("arrInstances.toasts.createFailed", { error: error.message || t("arrInstances.unknownError") }))
    },
  })

  const updateMutation = useMutation({
    mutationFn: async ({ id, data }: { id: number; data: ArrInstanceUpdateData }) => {
      return api.updateArrInstance(id, data)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["arrInstances"] })
      setEditInstance(null)
      toast.success(t("arrInstances.toasts.updated"))
    },
    onError: (error: Error) => {
      toast.error(t("arrInstances.toasts.updateFailed", { error: error.message || t("arrInstances.unknownError") }))
    },
  })

  const deleteMutation = useMutation({
    mutationFn: async (id: number) => {
      return api.deleteArrInstance(id)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["arrInstances"] })
      setDeleteInstance(null)
      toast.success(t("arrInstances.toasts.deleted"))
    },
    onError: (error: Error) => {
      toast.error(t("arrInstances.toasts.deleteFailed", { error: error.message || t("arrInstances.unknownError") }))
    },
  })

  const testMutation = useMutation({
    mutationFn: (id: number) => api.testArrInstance(id),
    onMutate: (id: number) => {
      setTestingId(id)
    },
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ["arrInstances"] })
      if (result.success) {
        toast.success(t("arrInstances.toasts.connectionSuccess"))
      } else {
        toast.error(t("arrInstances.toasts.connectionFailed", { error: result.error || t("arrInstances.unknownError") }))
      }
    },
    onError: (error: Error) => {
      toast.error(t("arrInstances.toasts.testFailed", { error: error.message || t("arrInstances.unknownError") }))
    },
    onSettled: () => {
      setTestingId(null)
    },
  })

  // Group instances by type
  const sonarrInstances = instances?.filter(i => i.type === "sonarr") ?? []
  const radarrInstances = instances?.filter(i => i.type === "radarr") ?? []

  const renderInstanceCard = (instance: ArrInstance) => (
    <Card className="bg-muted/40" key={instance.id}>
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between">
          <div className="space-y-1 flex-1 min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <CardTitle className="text-lg truncate">{instance.name}</CardTitle>
              <Badge variant={instance.enabled ? "default" : "secondary"}>
                {instance.enabled ? t("arrInstances.badges.enabled") : t("arrInstances.badges.disabled")}
              </Badge>
              {instance.last_test_status === "ok" && (
                <Badge variant="outline" className="text-green-500 border-green-500/50">
                  <CheckCircle className="h-3 w-3 mr-1" />
                  {t("arrInstances.badges.connected")}
                </Badge>
              )}
              {instance.last_test_status === "error" && (
                <Badge variant="outline" className="text-red-500 border-red-500/50">
                  <XCircle className="h-3 w-3 mr-1" />
                  {t("arrInstances.badges.failed")}
                </Badge>
              )}
            </div>
            <CardDescription className="text-xs truncate">
              {instance.base_url}
            </CardDescription>
            <CardDescription className="text-xs">
              {t("arrInstances.created", { date: formatDate(new Date(instance.created_at)) })}
              {instance.last_test_at && ` • Tested ${formatDate(new Date(instance.last_test_at))}`}
            </CardDescription>
          </div>
          <div className="flex gap-1 flex-shrink-0">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => testMutation.mutate(instance.id)}
              disabled={testingId === instance.id}
              aria-label={t("arrInstances.ariaLabels.testConnection", { name: instance.name })}
            >
              {testingId === instance.id ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Zap className="h-4 w-4" />
              )}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setEditInstance(instance)}
              aria-label={t("arrInstances.ariaLabels.edit", { name: instance.name })}
            >
              <Edit className="h-4 w-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setDeleteInstance(instance)}
              aria-label={t("arrInstances.ariaLabels.delete", { name: instance.name })}
            >
              <Trash2 className="h-4 w-4 text-destructive" />
            </Button>
          </div>
        </div>
      </CardHeader>
      {instance.last_test_error && (
        <CardContent className="pt-0">
          <div className="text-xs text-destructive bg-destructive/10 p-2 rounded">
            {instance.last_test_error}
          </div>
        </CardContent>
      )}
    </Card>
  )

  return (
    <div className="space-y-6">
      <div className="flex flex-col items-stretch gap-2 sm:flex-row sm:justify-end">
        <Dialog open={showCreateDialog} onOpenChange={setShowCreateDialog}>
          <DialogTrigger asChild>
            <Button size="sm" className="w-full sm:w-auto">
              <Plus className="mr-2 h-4 w-4" />
              {t("arrInstances.addButton")}
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-lg max-w-full max-h-[90dvh] flex flex-col">
            <DialogHeader className="flex-shrink-0">
              <DialogTitle>{t("arrInstances.addDialog.title")}</DialogTitle>
              <DialogDescription>
                {t("arrInstances.addDialog.description")}
              </DialogDescription>
            </DialogHeader>
            <div className="flex-1 overflow-y-auto min-h-0">
              <ArrInstanceForm
                onSubmit={(data) => createMutation.mutate(data as ArrInstanceFormData)}
                onCancel={() => setShowCreateDialog(false)}
                isPending={createMutation.isPending}
              />
            </div>
          </DialogContent>
        </Dialog>
      </div>

      {isLoading && <div className="text-center py-8">{t("arrInstances.loading")}</div>}
      {error && (
        <Card>
          <CardContent className="pt-6">
            <div className="text-destructive">{t("arrInstances.loadFailed")}</div>
          </CardContent>
        </Card>
      )}

      {!isLoading && !error && (!instances || instances.length === 0) && (
        <Card>
          <CardContent className="pt-6">
            <div className="text-center text-muted-foreground">
              {t("arrInstances.empty")}
            </div>
          </CardContent>
        </Card>
      )}

      {sonarrInstances.length > 0 && (
        <div className="space-y-3">
          <h3 className="text-sm font-medium text-muted-foreground">{t("arrInstances.sonarrInstances")}</h3>
          <div className="grid gap-3">
            {sonarrInstances.map(renderInstanceCard)}
          </div>
        </div>
      )}

      {radarrInstances.length > 0 && (
        <div className="space-y-3">
          <h3 className="text-sm font-medium text-muted-foreground">{t("arrInstances.radarrInstances")}</h3>
          <div className="grid gap-3">
            {radarrInstances.map(renderInstanceCard)}
          </div>
        </div>
      )}

      {/* Edit Dialog */}
      {editInstance && (
        <Dialog open={true} onOpenChange={() => setEditInstance(null)}>
          <DialogContent className="sm:max-w-lg max-w-full max-h-[90dvh] flex flex-col">
            <DialogHeader className="flex-shrink-0">
              <DialogTitle>{t("arrInstances.editDialog.title")}</DialogTitle>
            </DialogHeader>
            <div className="flex-1 overflow-y-auto min-h-0">
              <ArrInstanceForm
                instance={editInstance}
                onSubmit={(data) => updateMutation.mutate({ id: editInstance.id, data })}
                onCancel={() => setEditInstance(null)}
                isPending={updateMutation.isPending}
              />
            </div>
          </DialogContent>
        </Dialog>
      )}

      {/* Delete Confirmation Dialog */}
      <AlertDialog open={deleteInstance !== null} onOpenChange={() => setDeleteInstance(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("arrInstances.deleteDialog.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("arrInstances.deleteDialog.description", { name: deleteInstance?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("arrInstances.actions.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => deleteInstance && deleteMutation.mutate(deleteInstance.id)}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t("arrInstances.actions.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

interface ArrInstanceFormProps {
  instance?: ArrInstance
  onSubmit: (data: ArrInstanceFormData | ArrInstanceUpdateData) => void
  onCancel: () => void
  isPending: boolean
}

function ArrInstanceForm({ instance, onSubmit, onCancel, isPending }: ArrInstanceFormProps) {
  const { t } = useTranslation("settings")
  const [type, setType] = useState<ArrInstanceType>(instance?.type || "sonarr")
  const [name, setName] = useState(instance?.name || "")
  const [baseUrl, setBaseUrl] = useState(instance?.base_url || "")
  const [apiKey, setApiKey] = useState("")
  const [showBasicAuth, setShowBasicAuth] = useState(!!instance?.basic_username)
  const [basicUsername, setBasicUsername] = useState(instance?.basic_username ?? "")
  const [basicPassword, setBasicPassword] = useState(instance?.basic_username ? "<redacted>" : "")
  const [enabled, setEnabled] = useState(instance?.enabled !== false)
  const [priority, setPriority] = useState(instance?.priority ?? 0)
  const [timeoutSeconds, setTimeoutSeconds] = useState(instance?.timeout_seconds ?? 15)
  const [isTesting, setIsTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ success: boolean; error?: string } | null>(null)

  const isEdit = !!instance

  const handleTestConnection = async () => {
    if (!baseUrl.trim() || !apiKey.trim()) {
      toast.error(t("arrInstances.validation.baseUrlAndApiKeyRequired"))
      return
    }

    const trimmedBasicUser = basicUsername.trim()
    const trimmedBasicPass = basicPassword
    if (showBasicAuth) {
      if (!trimmedBasicUser) {
        toast.error(t("arrInstances.validation.basicUsernameRequired"))
        return
      }
      if (!trimmedBasicPass || trimmedBasicPass === "<redacted>") {
        toast.error(t("arrInstances.validation.basicPasswordRequired"))
        return
      }
    }

    setIsTesting(true)
    setTestResult(null)

    try {
      const result = await api.testArrConnection({
        type,
        base_url: baseUrl.trim(),
        api_key: apiKey.trim(),
        basic_username: showBasicAuth ? trimmedBasicUser : undefined,
        basic_password: showBasicAuth ? trimmedBasicPass : undefined,
      })
      setTestResult(result)
      if (result.success) {
        toast.success(t("arrInstances.toasts.connectionSuccess"))
      } else {
        toast.error(t("arrInstances.toasts.connectionFailed", { error: result.error || t("arrInstances.unknownError") }))
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : t("arrInstances.unknownError")
      setTestResult({ success: false, error: message })
      toast.error(t("arrInstances.toasts.testFailed", { error: message }))
    } finally {
      setIsTesting(false)
    }
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()

    if (!name.trim()) {
      toast.error(t("arrInstances.validation.nameRequired"))
      return
    }

    if (!baseUrl.trim()) {
      toast.error(t("arrInstances.validation.baseUrlRequired"))
      return
    }

    const trimmedBasicUser = basicUsername.trim()
    const trimmedBasicPass = basicPassword
    if (showBasicAuth) {
      if (!trimmedBasicUser) {
        toast.error(t("arrInstances.validation.basicUsernameRequired"))
        return
      }
      if (!isEdit && !trimmedBasicPass) {
        toast.error(t("arrInstances.validation.basicPasswordRequiredSubmit"))
        return
      }
      if (isEdit && trimmedBasicPass === "") {
        toast.error(t("arrInstances.validation.basicPasswordKeepOrEnter"))
        return
      }
    }

    if (!isEdit && !apiKey.trim()) {
      toast.error(t("arrInstances.validation.apiKeyRequired"))
      return
    }

    if (isEdit) {
      const updateData: ArrInstanceUpdateData = {
        name: name.trim(),
        base_url: baseUrl.trim(),
        enabled,
        priority,
        timeout_seconds: timeoutSeconds,
      }
      if (apiKey.trim()) {
        updateData.api_key = apiKey.trim()
      }
      if (showBasicAuth) {
        updateData.basic_username = trimmedBasicUser
        if (trimmedBasicPass !== "<redacted>") {
          updateData.basic_password = trimmedBasicPass
        }
      } else {
        updateData.basic_username = ""
        updateData.basic_password = ""
      }
      onSubmit(updateData)
    } else {
      const createData: ArrInstanceFormData = {
        type,
        name: name.trim(),
        base_url: baseUrl.trim(),
        api_key: apiKey.trim(),
        enabled,
        priority,
        timeout_seconds: timeoutSeconds,
      }
      if (showBasicAuth) {
        createData.basic_username = trimmedBasicUser
        createData.basic_password = trimmedBasicPass
      }
      onSubmit(createData)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      {!isEdit && (
        <div className="space-y-2">
          <Label htmlFor="type">{t("arrInstances.form.type")}</Label>
          <Select value={type} onValueChange={(v) => setType(v as ArrInstanceType)}>
            <SelectTrigger>
              <SelectValue placeholder={t("arrInstances.form.selectType")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="sonarr">{t("arrInstances.form.types.sonarr")}</SelectItem>
              <SelectItem value="radarr">{t("arrInstances.form.types.radarr")}</SelectItem>
            </SelectContent>
          </Select>
        </div>
      )}

      <div className="space-y-2">
        <Label htmlFor="name">{t("arrInstances.form.name")}</Label>
        <Input
          id="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("arrInstances.form.namePlaceholder", {
            type: type === "sonarr" ? t("arrInstances.form.types.sonarrShort") : t("arrInstances.form.types.radarrShort"),
          })}
          required
        />
      </div>

      <div className="space-y-2">
        <Label htmlFor="baseUrl">{t("arrInstances.form.baseUrl")}</Label>
        <Input
          id="baseUrl"
          value={baseUrl}
          onChange={(e) => setBaseUrl(e.target.value)}
          placeholder={`http://localhost:${type === "sonarr" ? "8989" : "7878"}`}
          required
        />
        <p className="text-xs text-muted-foreground">
          {t("arrInstances.form.baseUrlDescription", {
            type: type === "sonarr" ? t("arrInstances.form.types.sonarrShort") : t("arrInstances.form.types.radarrShort"),
          })}
        </p>
      </div>

      <div className="space-y-2">
        <Label htmlFor="apiKey">
          {isEdit ? t("arrInstances.form.apiKeyOptional") : t("arrInstances.form.apiKey")}
        </Label>
        <Input
          id="apiKey"
          type="password"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          placeholder={isEdit ? t("arrInstances.form.apiKeyPlaceholderEdit") : t("arrInstances.form.apiKeyPlaceholder")}
          required={!isEdit}
        />
        <p className="text-xs text-muted-foreground">
          {t("arrInstances.form.apiKeyDescription", {
            type: type === "sonarr" ? t("arrInstances.form.types.sonarrShort") : t("arrInstances.form.types.radarrShort"),
          })}
        </p>
      </div>

      <div className="flex items-start justify-between gap-4 rounded-lg border bg-muted/40 p-4">
        <div className="space-y-1">
          <Label htmlFor="arr-basic-auth">{t("arrInstances.form.basicAuth")}</Label>
          <p className="text-sm text-muted-foreground max-w-prose">
            {t("arrInstances.form.basicAuthDescription")}
          </p>
        </div>
        <Switch
          id="arr-basic-auth"
          checked={showBasicAuth}
          onCheckedChange={(checked) => {
            setShowBasicAuth(checked)
            if (!checked) {
              setBasicUsername("")
              setBasicPassword("")
            } else if (!basicUsername.trim()) {
              setBasicPassword("")
            }
          }}
        />
      </div>

      {showBasicAuth && (
        <div className="grid gap-4 rounded-lg border bg-muted/20 p-4">
          <div className="grid gap-2">
            <Label htmlFor="basicUsername">{t("arrInstances.form.basicUsername")}</Label>
            <Input
              id="basicUsername"
              value={basicUsername}
              onChange={(e) => setBasicUsername(e.target.value)}
              placeholder={t("arrInstances.form.basicUsernamePlaceholder")}
              autoComplete="off"
              data-1p-ignore
              required
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="basicPassword">{t("arrInstances.form.basicPassword")}</Label>
            <Input
              id="basicPassword"
              type="password"
              value={basicPassword}
              onChange={(e) => setBasicPassword(e.target.value)}
              placeholder={isEdit ? t("arrInstances.form.basicPasswordPlaceholderEdit") : t("arrInstances.form.basicPasswordPlaceholder")}
              autoComplete="off"
              data-1p-ignore
              required={!isEdit}
            />
            {isEdit && (
              <p className="text-xs text-muted-foreground">
                {t("arrInstances.form.basicPasswordKeep")}
              </p>
            )}
          </div>
        </div>
      )}

      <div className="grid grid-cols-2 gap-4">
        <div className="space-y-2">
          <Label htmlFor="priority">{t("arrInstances.form.priority")}</Label>
          <Input
            id="priority"
            type="number"
            value={priority}
            onChange={(e) => setPriority(parseInt(e.target.value) || 0)}
            min={0}
          />
          <p className="text-xs text-muted-foreground">
            {t("arrInstances.form.priorityDescription")}
          </p>
        </div>

        <div className="space-y-2">
          <Label htmlFor="timeout">{t("arrInstances.form.timeoutSeconds")}</Label>
          <Input
            id="timeout"
            type="number"
            value={timeoutSeconds}
            onChange={(e) => setTimeoutSeconds(parseInt(e.target.value) || 15)}
            min={1}
            max={120}
          />
        </div>
      </div>

      <div className="flex items-center space-x-2">
        <Switch
          id="enabled"
          checked={enabled}
          onCheckedChange={setEnabled}
        />
        <Label htmlFor="enabled" className="cursor-pointer">
          {t("arrInstances.form.enabled")}
        </Label>
      </div>

      {testResult && (
        <div className={`text-sm p-2 rounded ${testResult.success ? "bg-green-500/10 text-green-500" : "bg-destructive/10 text-destructive"}`}>
          {testResult.success? t("arrInstances.toasts.connectionSuccess"): t("arrInstances.toasts.connectionFailed", { error: testResult.error ?? t("arrInstances.unknownError") })}
        </div>
      )}

      <div className="flex justify-between gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={handleTestConnection}
          disabled={isTesting || !baseUrl.trim() || !apiKey.trim()}
        >
          {isTesting ? (
            <>
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t("arrInstances.actions.testing")}
            </>
          ) : (
            <>
              <Zap className="mr-2 h-4 w-4" />
              {t("arrInstances.actions.testConnection")}
            </>
          )}
        </Button>
        <div className="flex gap-2">
          <Button type="button" variant="outline" onClick={onCancel} disabled={isPending}>
            {t("arrInstances.actions.cancel")}
          </Button>
          <Button type="submit" disabled={isPending}>
            {isPending ? t("arrInstances.actions.saving") : isEdit ? t("arrInstances.actions.update") : t("arrInstances.actions.create")}
          </Button>
        </div>
      </div>
    </form>
  )
}
