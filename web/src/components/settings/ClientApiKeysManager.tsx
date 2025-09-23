/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMemo, useState } from "react"
import type { ReactNode } from "react"
import { useForm } from "@tanstack/react-form"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { formatDate } from "@/lib/utils"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Copy, Plus, Trash2, Server, Globe } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from "@/components/ui/dialog"
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

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

const INLINE_CODE_CLASS = "rounded bg-muted px-1 py-0.5 font-mono text-xs"
const DEFAULT_QUI_URL_EXAMPLE = "http://localhost:7476"
const ARR_CLIENTS = new Set(["sonarr", "radarr", "lidarr"])

function renderProxyInstructions(clientName: string, proxyUrl: string, instanceHost?: string): ReactNode {
  const normalizedName = clientName.trim().toLowerCase()

  if (ARR_CLIENTS.has(normalizedName)) {
    return (
      <dl className="space-y-1.5 text-xs">
        <div className="flex gap-2">
          <dt className="font-medium text-muted-foreground min-w-20">Host:</dt>
          <dd className="text-foreground">Your qui server (e.g., 100.69.80.112 or domain.tld)</dd>
        </div>
        <div className="flex gap-2">
          <dt className="font-medium text-muted-foreground min-w-20">Port:</dt>
          <dd className="text-foreground">Your qui port</dd>
        </div>
        <div className="flex gap-2">
          <dt className="font-medium text-muted-foreground min-w-20">URL Base:</dt>
          <dd className="text-foreground">
            <code className={INLINE_CODE_CLASS}>{proxyUrl}</code>
            <span className="text-muted-foreground ml-1">(Show Advanced)</span>
          </dd>
        </div>
      </dl>
    )
  }

  if (normalizedName === "autobrr") {
    return (
      <div className="text-xs space-y-1.5">
        <p className="text-foreground">
          In Autobrr, set the qBittorrent host to:
        </p>
        <p className="text-muted-foreground">
          <span className="text-foreground">Your qui server URL</span> + <code className={INLINE_CODE_CLASS}>{proxyUrl}</code>
        </p>
        <p className="text-muted-foreground">
          Example: <code className={INLINE_CODE_CLASS}>{`${DEFAULT_QUI_URL_EXAMPLE}${proxyUrl}`}</code>
        </p>
      </div>
    )
  }

  const hostHint = instanceHost ? (
    <code className={INLINE_CODE_CLASS}>{instanceHost}</code>
  ) : (
    <span>your current qBittorrent host</span>
  )

  return (
    <div className="text-xs space-y-1.5">
      <p className="text-foreground">
        Replace {hostHint} with:
      </p>
      <p className="text-muted-foreground">
        <span className="text-foreground">Your qui server URL</span> + <code className={INLINE_CODE_CLASS}>{proxyUrl}</code>
      </p>
      <p className="text-muted-foreground">
        Example: <code className={INLINE_CODE_CLASS}>{`${DEFAULT_QUI_URL_EXAMPLE}${proxyUrl}`}</code>
      </p>
    </div>
  )
}

export function ClientApiKeysManager() {
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [deleteKeyId, setDeleteKeyId] = useState<number | null>(null)
  const [newKey, setNewKey] = useState<NewClientAPIKey | null>(null)
  const queryClient = useQueryClient()

  const instructionsContent = useMemo(() => {
    if (!newKey) {
      return null
    }

    return renderProxyInstructions(
      newKey.clientApiKey.clientName,
      newKey.proxyUrl,
      newKey.instance?.host
    )
  }, [newKey])

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
      toast.success("Client API key created successfully")
    },
    onError: () => {
      toast.error("Failed to create client API key")
    },
  })

  const deleteMutation = useMutation({
    mutationFn: async (id: number) => {
      return api.deleteClientApiKey(id)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["clientApiKeys"] })
      setDeleteKeyId(null)
      toast.success("Client API key deleted successfully")
    },
    onError: (error) => {
      console.error("Delete client API key error:", error)
      toast.error(`Failed to delete client API key: ${error.message || "Unknown error"}`)
    },
  })

  const form = useForm({
    defaultValues: {
      clientName: "",
      instanceId: "",
    },
    onSubmit: async ({ value }) => {
      const instanceId = parseInt(value.instanceId)
      if (!instanceId) {
        toast.error("Please select an instance")
        return
      }

      await createMutation.mutateAsync({
        clientName: value.clientName,
        instanceId,
      })
      form.reset()
    },
  })

  const commonClientNames = [
    "autobrr",
    "Sonarr",
    "Radarr",
    "Lidarr",
    "Prowlarr",
    "Bazarr",
    "Readarr",
  ]

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">
          Client API keys allow external applications like autobrr and Sonarr/Radarr to connect through qui to your qBittorrent instances.
        </p>
        <Dialog open={showCreateDialog} onOpenChange={setShowCreateDialog}>
          <DialogTrigger asChild>
            <Button size="sm">
              <Plus className="mr-2 h-4 w-4" />
              Create Client API Key
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-2xl">
            <DialogHeader>
              <DialogTitle>Create Client API Key</DialogTitle>
              <DialogDescription>
                Create an API key for a specific client to connect to a qBittorrent instance.
              </DialogDescription>
            </DialogHeader>

            {newKey ? (
              <div className="space-y-4">
                <Card>
                  <CardHeader className="pb-3">
                    <CardTitle className="text-base">API Key Created</CardTitle>
                    <CardDescription>
                      Save this proxy URL now. You won't be able to see it again.
                    </CardDescription>
                  </CardHeader>
                  <CardContent className="space-y-3">
                    <div>
                      <Label htmlFor="proxy-url" className="text-xs uppercase text-muted-foreground">Proxy URL</Label>
                      <div className="mt-1 flex items-center gap-2">
                        <code id="proxy-url" className="flex-1 rounded bg-muted px-2 py-1.5 text-xs font-mono break-all">
                          {newKey.proxyUrl}
                        </code>
                        <Button
                          size="icon"
                          variant="outline"
                          className="h-7 w-7"
                          onClick={() => {
                            navigator.clipboard.writeText(newKey.proxyUrl)
                            toast.success("Proxy URL copied to clipboard")
                          }}
                          title="Copy proxy URL"
                        >
                          <Copy className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </div>

                    <div className="border rounded-md p-3 space-y-2">
                      <h4 className="text-xs font-semibold uppercase text-muted-foreground">Setup Instructions</h4>
                      <div className="text-sm space-y-1">
                        {instructionsContent}
                      </div>
                    </div>
                  </CardContent>
                </Card>

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
                  name="clientName"
                  validators={{
                    onChange: ({ value }) => !value ? "Client name is required" : undefined,
                  }}
                >
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor="clientName">Client Name</Label>
                      <div className="space-y-2">
                        <Input
                          id="clientName"
                          placeholder="e.g., Sonarr, Radarr"
                          value={field.state.value}
                          onBlur={field.handleBlur}
                          onChange={(e) => field.handleChange(e.target.value)}
                          data-1p-ignore
                          autoComplete='off'
                        />
                        <div className="flex flex-wrap gap-1">
                          {commonClientNames.map((name) => (
                            <Button
                              key={name}
                              type="button"
                              variant="outline"
                              size="sm"
                              className="h-6 text-xs"
                              onClick={() => field.handleChange(name)}
                            >
                              {name}
                            </Button>
                          ))}
                        </div>
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
                    onChange: ({ value }) => !value ? "Instance is required" : undefined,
                  }}
                >
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor="instanceId">qBittorrent Instance</Label>
                      <Select
                        value={field.state.value}
                        onValueChange={(value) => field.handleChange(value)}
                      >
                        <SelectTrigger>
                          <SelectValue placeholder="Select an instance" />
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
                      {isSubmitting || createMutation.isPending ? "Creating..." : "Create Client API Key"}
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
            Loading client API keys...
          </p>
        ) : error ? (
          <div className="text-center py-8">
            <p className="text-sm text-muted-foreground mb-2">
              Unable to load client API keys
            </p>
            <p className="text-xs text-destructive">
              {error.message?.includes("404")? "Feature may not be available in this version": error.message || "An error occurred"
              }
            </p>
          </div>
        ) : (
          <>
            {keys.map((key) => (
              <div
                key={key.id}
                className="flex items-center justify-between rounded-lg border p-4"
              >
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{key.clientName}</span>
                    <Badge variant="outline" className="text-xs">
                      ID: {key.id}
                    </Badge>
                    {key.instance ? (
                      <Badge variant="secondary" className="text-xs flex items-center gap-1">
                        <Server className="h-3 w-3" />
                        {key.instance.name}
                      </Badge>
                    ) : (
                      <Badge variant="destructive" className="text-xs">
                        Instance Deleted
                      </Badge>
                    )}
                  </div>
                  <div className="text-sm text-muted-foreground space-y-1">
                    <p>
                      Created: {formatDate(new Date(key.createdAt).getTime() / 1000)}
                      {key.lastUsedAt && (
                        <> • Last used: {formatDate(new Date(key.lastUsedAt).getTime() / 1000)}</>
                      )}
                    </p>
                    {key.instance && (
                      <div className="flex items-center gap-1 text-xs">
                        <Globe className="h-3 w-3" />
                        <code className="bg-muted px-1 rounded">/proxy/{"{api-key}"}</code>
                        <span>→</span>
                        <span>{key.instance.host}</span>
                      </div>
                    )}
                  </div>
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
                No client API keys created yet
              </p>
            )}
          </>
        )}
      </div>

      <AlertDialog open={!!deleteKeyId} onOpenChange={() => setDeleteKeyId(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Client API Key?</AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. Any applications using this key will lose access to the qBittorrent instance.
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
