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
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { api } from "@/lib/api"
import { getBaseUrl } from "@/lib/base-url"
import { useIncognitoMode } from "@/lib/incognito"
import { copyTextToClipboard } from "@/lib/utils"
import { useForm } from "@tanstack/react-form"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Copy, Eye, EyeOff, Plus, Server, Trash2 } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface NewClientAPIKey {
  key: string
  clientApiKey: {
    id: number
    clientName: string
    instanceId: number
    createdAt: string
  }
  instance?: {
    id: number
    name: string
    host: string
  }
  proxyUrl: string
}

// Helper function to truncate long instance names
function truncateInstanceName(name: string, maxLength = 20): string {
  if (name.length <= maxLength) return name
  return `${name.slice(0, maxLength)}...`
}

export function ClientApiKeysManager() {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [deleteKeyId, setDeleteKeyId] = useState<number | null>(null)
  const [newKey, setNewKey] = useState<NewClientAPIKey | null>(null)
  const queryClient = useQueryClient()
  const { formatDate } = useDateTimeFormatters()
  const [incognitoMode, setIncognitoMode] = useIncognitoMode()

  // Get the current browser URL to construct full proxy URL
  const resolveProxyPath = (proxyPath: string) => {
    const normalizedProxyPath = proxyPath.startsWith("/") ? proxyPath : `/${proxyPath}`
    const base = getBaseUrl()
    const baseWithoutTrailingSlash = base.endsWith("/") ? base.slice(0, -1) : base

    if (!baseWithoutTrailingSlash) {
      return normalizedProxyPath
    }

    if (
      normalizedProxyPath === baseWithoutTrailingSlash ||
      normalizedProxyPath.startsWith(`${baseWithoutTrailingSlash}/`)
    ) {
      return normalizedProxyPath
    }

    return `${baseWithoutTrailingSlash}${normalizedProxyPath}`
  }

  const getFullProxyUrl = (proxyPath: string) => {
    const { protocol, hostname, port } = window.location
    const origin = port ? `${protocol}//${hostname}:${port}` : `${protocol}//${hostname}`
    return `${origin}${resolveProxyPath(proxyPath)}`
  }

  // Fetch client API keys
  const { data: clientApiKeys, isLoading, error } = useQuery({
    queryKey: ["clientApiKeys"],
    queryFn: () => api.getClientApiKeys(),
    staleTime: 30 * 1000, // 30 seconds
    retry: (failureCount, error) => {
      // Don't retry on 404 - endpoint might not be available
      if (error && error.message?.includes("404")) {
        return false
      }
      return failureCount < 3
    },
  })

  // Fetch instances for the dropdown
  const { data: instances } = useQuery({
    queryKey: ["instances"],
    queryFn: () => api.getInstances(),
    staleTime: 60 * 1000, // 1 minute
  })

  // Ensure clientApiKeys is always an array
  const keys = clientApiKeys || []

  const createMutation = useMutation({
    mutationFn: async (data: { clientName: string; instanceId: number }) => {
      return api.createClientApiKey(data)
    },
    onSuccess: (data) => {
      setNewKey(data)
      queryClient.invalidateQueries({ queryKey: ["clientApiKeys"] })
      toast.success(tr("clientApiKeysManager.toasts.created"))
    },
    onError: () => {
      toast.error(tr("clientApiKeysManager.toasts.failedCreate"))
    },
  })

  const deleteMutation = useMutation({
    mutationFn: async (id: number) => {
      return api.deleteClientApiKey(id)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["clientApiKeys"] })
      setDeleteKeyId(null)
      toast.success(tr("clientApiKeysManager.toasts.deleted"))
    },
    onError: (error) => {
      console.error("Delete client API key error:", error)
      toast.error(tr("clientApiKeysManager.toasts.failedDelete", { error: error.message || tr("clientApiKeysManager.common.unknownError") }))
    },
  })

  const form = useForm({
    defaultValues: {
      clientName: "",
      instanceId: "",
    },
    onSubmit: async ({ value }) => {
      const clientName = value.clientName.trim()
      if (clientName === "") {
        toast.error(tr("clientApiKeysManager.form.errors.clientNameRequired"))
        return
      }

      const instanceId = parseInt(value.instanceId, 10)
      if (!instanceId) {
        toast.error(tr("clientApiKeysManager.form.errors.selectInstance"))
        return
      }

      await createMutation.mutateAsync({
        clientName,
        instanceId,
      })
      form.reset()
    },
  })

  const handleDialogOpenChange = (open: boolean) => {
    setShowCreateDialog(open)
    if (!open) {
      setNewKey(null)
      form.reset()
    }
  }

  return (
    <TooltipProvider>
      <div className="space-y-4">
        <div className="flex flex-col items-stretch gap-2 sm:flex-row sm:justify-end">
          <Dialog open={showCreateDialog} onOpenChange={handleDialogOpenChange}>
            <DialogTrigger asChild>
              <Button size="sm" className="w-full sm:w-auto">
                <Plus className="mr-2 h-4 w-4" />
                {tr("clientApiKeysManager.actions.create")}
              </Button>
            </DialogTrigger>
            <DialogContent className="sm:max-w-xl max-w-full max-h-[90dvh] flex flex-col">
              <DialogHeader className="flex-shrink-0">
                <DialogTitle>{tr("clientApiKeysManager.dialogs.createTitle")}</DialogTitle>
                <DialogDescription>
                  {tr("clientApiKeysManager.dialogs.createDescription")}
                </DialogDescription>
              </DialogHeader>

              <div className="flex-1 overflow-y-auto min-h-0">
                {newKey ? (
                  <div className="space-y-4">
                    <Card className="w-full">
                      <CardHeader>
                        <CardTitle className="text-base">{tr("clientApiKeysManager.dialogs.keyCreatedTitle")}</CardTitle>
                        <CardDescription>
                          {tr("clientApiKeysManager.dialogs.keyCreatedDescription")}
                        </CardDescription>
                      </CardHeader>
                      <CardContent className="space-y-3">
                        <div>
                          <Label htmlFor="proxy-url" className="text-xs uppercase text-muted-foreground">{tr("clientApiKeysManager.labels.proxyUrl")}</Label>
                          <div className="mt-1 flex flex-wrap items-center gap-2 sm:flex-nowrap">
                            <code
                              id="proxy-url"
                              className={`w-full flex-1 rounded bg-muted px-2 py-1.5 text-xs font-mono break-all ${incognitoMode ? "blur-sm select-none" : ""}`}
                            >
                              {getFullProxyUrl(newKey.proxyUrl)}
                            </code>
                            <Button
                              size="icon"
                              variant="outline"
                              className="h-7 w-7"
                              onClick={() => setIncognitoMode(!incognitoMode)}
                              title={incognitoMode
                                ? tr("clientApiKeysManager.actions.showProxyUrl")
                                : tr("clientApiKeysManager.actions.hideProxyUrl")}
                            >
                              {incognitoMode ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                            </Button>
                            <Button
                              size="icon"
                              variant="outline"
                              className="h-7 w-7"
                              onClick={async () => {
                                try {
                                  await copyTextToClipboard(getFullProxyUrl(newKey.proxyUrl))
                                  toast.success(tr("clientApiKeysManager.toasts.proxyUrlCopied"))
                                } catch {
                                  toast.error(tr("clientApiKeysManager.toasts.failedCopy"))
                                }
                              }}
                              title={tr("clientApiKeysManager.actions.copyProxyUrl")}
                            >
                              <Copy className="h-3.5 w-3.5" />
                            </Button>
                          </div>
                        </div>
                      </CardContent>
                    </Card>

                    <Button
                      onClick={() => handleDialogOpenChange(false)}
                      className="w-full"
                    >
                      {tr("clientApiKeysManager.actions.done")}
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
                      name="clientName"
                      validators={{
                        onChange: ({ value }) => value.trim() === "" ? tr("clientApiKeysManager.form.errors.clientNameRequired") : undefined,
                      }}
                    >
                      {(field) => (
                        <div className="space-y-2">
                          <Label htmlFor="clientName">{tr("clientApiKeysManager.form.fields.clientName")}</Label>
                          <div className="space-y-2">
                            <Input
                              id="clientName"
                              placeholder={tr("clientApiKeysManager.form.placeholders.clientName")}
                              value={field.state.value}
                              onBlur={field.handleBlur}
                              onChange={(e) => field.handleChange(e.target.value)}
                              data-1p-ignore
                              autoComplete='off'
                            />
                          </div>
                          {field.state.meta.isTouched && field.state.meta.errors[0] && (
                            <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                          )}
                        </div>
                      )}
                    </form.Field>

                    <form.Field
                      name="instanceId"
                      validators={{
                        onChange: ({ value }) => value.trim() === "" ? tr("clientApiKeysManager.form.errors.instanceRequired") : undefined,
                      }}
                    >
                      {(field) => (
                        <div className="space-y-2">
                          <Label htmlFor="instanceId">{tr("clientApiKeysManager.form.fields.instance")}</Label>
                          <Select
                            value={field.state.value}
                            onValueChange={(value) => field.handleChange(value)}
                          >
                            <SelectTrigger>
                              <SelectValue placeholder={tr("clientApiKeysManager.form.placeholders.selectInstance")} />
                            </SelectTrigger>
                            <SelectContent>
                              {instances?.map((instance) => (
                                <SelectItem key={instance.id} value={instance.id.toString()}>
                                  <div className="flex items-center gap-2">
                                    <Server className="h-4 w-4" />
                                    <span>{instance.name}</span>
                                    <span className="text-xs text-muted-foreground">({instance.host})</span>
                                  </div>
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
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
                          {isSubmitting || createMutation.isPending
                            ? tr("clientApiKeysManager.actions.creating")
                            : tr("clientApiKeysManager.actions.create")}
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
              {tr("clientApiKeysManager.states.loading")}
            </p>
          ) : error ? (
            <div className="text-center py-8">
              <p className="text-sm text-muted-foreground mb-2">
                {tr("clientApiKeysManager.states.unableLoad")}
              </p>
              <p className="text-xs text-destructive">
                {error.message?.includes("404")
                  ? tr("clientApiKeysManager.states.featureUnavailable")
                  : error.message || tr("clientApiKeysManager.common.genericError")
                }
              </p>
            </div>
          ) : (
            <>
              {keys.map((key) => (
                <div
                  key={key.id}
                  className="rounded-lg border bg-muted/40 p-4 transition-colors"
                >
                  <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                    <div className="space-y-3">
                      <div className="flex flex-wrap items-center gap-2 text-sm">
                        <span className="font-medium text-base sm:text-lg">{key.clientName}</span>
                        <Badge variant="outline" className="text-xs">
                          {tr("clientApiKeysManager.labels.id", { id: key.id })}
                        </Badge>
                        {key.instance ? (
                          key.instance.name.length > 20 ? (
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <Badge variant="secondary" className="text-xs flex items-center gap-1 cursor-help">
                                  <Server className="h-3 w-3 shrink-0" />
                                  <span className="truncate">{truncateInstanceName(key.instance.name)}</span>
                                </Badge>
                              </TooltipTrigger>
                              <TooltipContent>
                                <p>{key.instance.name}</p>
                              </TooltipContent>
                            </Tooltip>
                          ) : (
                            <Badge variant="secondary" className="text-xs flex items-center gap-1">
                              <Server className="h-3 w-3 shrink-0" />
                              {key.instance.name}
                            </Badge>
                          )
                        ) : (
                          <Badge variant="destructive" className="text-xs">
                            {tr("clientApiKeysManager.labels.instanceDeleted")}
                          </Badge>
                        )}
                      </div>

                      <div className="space-y-1 text-xs text-muted-foreground">
                        <p className="flex flex-wrap items-center gap-1">
                          <span className="text-foreground">{tr("clientApiKeysManager.labels.created")}</span>
                          <span>{formatDate(new Date(key.createdAt))}</span>
                          {key.lastUsedAt && (
                            <>
                              <span>•</span>
                              <span className="text-foreground">{tr("clientApiKeysManager.labels.lastUsed")}</span>
                              <span>{formatDate(new Date(key.lastUsedAt))}</span>
                            </>
                          )}
                        </p>
                        {key.instance?.host && (
                          <div className="flex flex-wrap items-center gap-1 break-all">
                            <span className="text-foreground">{tr("clientApiKeysManager.labels.host")}</span>
                            <span className={incognitoMode ? "blur-sm select-none" : ""}>
                              {key.instance.host}
                            </span>
                            <Button
                              size="icon"
                              variant="ghost"
                              className="h-6 w-6 px-0 text-muted-foreground hover:text-foreground"
                              onClick={() => setIncognitoMode(!incognitoMode)}
                              title={incognitoMode ? tr("clientApiKeysManager.actions.showHost") : tr("clientApiKeysManager.actions.hideHost")}
                              aria-label={incognitoMode ? tr("clientApiKeysManager.actions.showHost") : tr("clientApiKeysManager.actions.hideHost")}
                            >
                              {incognitoMode ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                            </Button>
                          </div>
                        )}
                      </div>
                    </div>

                    <Button
                      size="icon"
                      variant="ghost"
                      className="h-9 w-9 self-end text-destructive hover:text-destructive focus-visible:ring-destructive sm:self-start"
                      onClick={() => setDeleteKeyId(key.id)}
                      aria-label={tr("clientApiKeysManager.aria.deleteKey", { name: key.clientName })}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              ))}

              {keys.length === 0 && (
                <p className="text-center text-sm text-muted-foreground py-8">
                  {tr("clientApiKeysManager.states.empty")}
                </p>
              )}
            </>
          )}
        </div>

        <AlertDialog open={!!deleteKeyId} onOpenChange={() => setDeleteKeyId(null)}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{tr("clientApiKeysManager.dialogs.deleteTitle")}</AlertDialogTitle>
              <AlertDialogDescription>
                {tr("clientApiKeysManager.dialogs.deleteDescription")}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{tr("clientApiKeysManager.actions.cancel")}</AlertDialogCancel>
              <AlertDialogAction
                onClick={() => deleteKeyId && deleteMutation.mutate(deleteKeyId)}
                className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              >
                {tr("clientApiKeysManager.actions.delete")}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </TooltipProvider>
  )
}
