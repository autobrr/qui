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
  SelectValue,
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
import { toast } from "sonner"

export function ArrInstancesManager() {
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
      toast.success("ARR instance created successfully")
    },
    onError: (error: Error) => {
      toast.error(`Failed to create ARR instance: ${error.message || "Unknown error"}`)
    },
  })

  const updateMutation = useMutation({
    mutationFn: async ({ id, data }: { id: number; data: ArrInstanceUpdateData }) => {
      return api.updateArrInstance(id, data)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["arrInstances"] })
      setEditInstance(null)
      toast.success("ARR instance updated successfully")
    },
    onError: (error: Error) => {
      toast.error(`Failed to update ARR instance: ${error.message || "Unknown error"}`)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: async (id: number) => {
      return api.deleteArrInstance(id)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["arrInstances"] })
      setDeleteInstance(null)
      toast.success("ARR instance deleted successfully")
    },
    onError: (error: Error) => {
      toast.error(`Failed to delete ARR instance: ${error.message || "Unknown error"}`)
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
        toast.success("Connection successful")
      } else {
        toast.error(`Connection failed: ${result.error || "Unknown error"}`)
      }
    },
    onError: (error: Error) => {
      toast.error(`Connection test failed: ${error.message || "Unknown error"}`)
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
                {instance.enabled ? "Enabled" : "Disabled"}
              </Badge>
              {instance.last_test_status === "ok" && (
                <Badge variant="outline" className="text-green-500 border-green-500/50">
                  <CheckCircle className="h-3 w-3 mr-1" />
                  Connected
                </Badge>
              )}
              {instance.last_test_status === "error" && (
                <Badge variant="outline" className="text-red-500 border-red-500/50">
                  <XCircle className="h-3 w-3 mr-1" />
                  Failed
                </Badge>
              )}
            </div>
            <CardDescription className="text-xs truncate">
              {instance.base_url}
            </CardDescription>
            <CardDescription className="text-xs">
              Created {formatDate(new Date(instance.created_at))}
              {instance.last_test_at && ` • Tested ${formatDate(new Date(instance.last_test_at))}`}
            </CardDescription>
          </div>
          <div className="flex gap-1 flex-shrink-0">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => testMutation.mutate(instance.id)}
              disabled={testingId === instance.id}
              aria-label={`Test connection for ${instance.name}`}
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
              aria-label={`Edit ${instance.name}`}
            >
              <Edit className="h-4 w-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setDeleteInstance(instance)}
              aria-label={`Delete ${instance.name}`}
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
              Add ARR Instance
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-lg max-w-full max-h-[90dvh] flex flex-col">
            <DialogHeader className="flex-shrink-0">
              <DialogTitle>Add ARR Instance</DialogTitle>
              <DialogDescription>
                Configure a Sonarr or Radarr instance for ID lookups during cross-seed searches.
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

      {isLoading && <div className="text-center py-8">Loading ARR instances...</div>}
      {error && (
        <Card>
          <CardContent className="pt-6">
            <div className="text-destructive">Failed to load ARR instances</div>
          </CardContent>
        </Card>
      )}

      {!isLoading && !error && (!instances || instances.length === 0) && (
        <Card>
          <CardContent className="pt-6">
            <div className="text-center text-muted-foreground">
              No ARR instances configured. Add a Sonarr or Radarr instance to enable ID-based cross-seed searches.
            </div>
          </CardContent>
        </Card>
      )}

      {sonarrInstances.length > 0 && (
        <div className="space-y-3">
          <h3 className="text-sm font-medium text-muted-foreground">Sonarr Instances</h3>
          <div className="grid gap-3">
            {sonarrInstances.map(renderInstanceCard)}
          </div>
        </div>
      )}

      {radarrInstances.length > 0 && (
        <div className="space-y-3">
          <h3 className="text-sm font-medium text-muted-foreground">Radarr Instances</h3>
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
              <DialogTitle>Edit ARR Instance</DialogTitle>
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
            <AlertDialogTitle>Delete ARR Instance</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete "{deleteInstance?.name}"? This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => deleteInstance && deleteMutation.mutate(deleteInstance.id)}
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

interface ArrInstanceFormProps {
  instance?: ArrInstance
  onSubmit: (data: ArrInstanceFormData | ArrInstanceUpdateData) => void
  onCancel: () => void
  isPending: boolean
}

function ArrInstanceForm({ instance, onSubmit, onCancel, isPending }: ArrInstanceFormProps) {
  const [type, setType] = useState<ArrInstanceType>(instance?.type || "sonarr")
  const [name, setName] = useState(instance?.name || "")
  const [baseUrl, setBaseUrl] = useState(instance?.base_url || "")
  const [apiKey, setApiKey] = useState("")
  const [enabled, setEnabled] = useState(instance?.enabled !== false)
  const [priority, setPriority] = useState(instance?.priority ?? 0)
  const [timeoutSeconds, setTimeoutSeconds] = useState(instance?.timeout_seconds ?? 15)
  const [isTesting, setIsTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ success: boolean; error?: string } | null>(null)

  const isEdit = !!instance

  const handleTestConnection = async () => {
    if (!baseUrl.trim() || !apiKey.trim()) {
      toast.error("Base URL and API Key are required to test connection")
      return
    }

    setIsTesting(true)
    setTestResult(null)

    try {
      const result = await api.testArrConnection({
        type,
        base_url: baseUrl.trim(),
        api_key: apiKey.trim(),
      })
      setTestResult(result)
      if (result.success) {
        toast.success("Connection successful")
      } else {
        toast.error(`Connection failed: ${result.error || "Unknown error"}`)
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : "Unknown error"
      setTestResult({ success: false, error: message })
      toast.error(`Connection test failed: ${message}`)
    } finally {
      setIsTesting(false)
    }
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()

    if (!name.trim()) {
      toast.error("Name is required")
      return
    }

    if (!baseUrl.trim()) {
      toast.error("Base URL is required")
      return
    }

    if (!isEdit && !apiKey.trim()) {
      toast.error("API Key is required")
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
      onSubmit(updateData)
    } else {
      onSubmit({
        type,
        name: name.trim(),
        base_url: baseUrl.trim(),
        api_key: apiKey.trim(),
        enabled,
        priority,
        timeout_seconds: timeoutSeconds,
      })
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      {!isEdit && (
        <div className="space-y-2">
          <Label htmlFor="type">Type *</Label>
          <Select value={type} onValueChange={(v) => setType(v as ArrInstanceType)}>
            <SelectTrigger>
              <SelectValue placeholder="Select type" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="sonarr">Sonarr (TV Shows)</SelectItem>
              <SelectItem value="radarr">Radarr (Movies)</SelectItem>
            </SelectContent>
          </Select>
        </div>
      )}

      <div className="space-y-2">
        <Label htmlFor="name">Name *</Label>
        <Input
          id="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={`My ${type === "sonarr" ? "Sonarr" : "Radarr"}`}
          required
        />
      </div>

      <div className="space-y-2">
        <Label htmlFor="baseUrl">Base URL *</Label>
        <Input
          id="baseUrl"
          value={baseUrl}
          onChange={(e) => setBaseUrl(e.target.value)}
          placeholder={`http://localhost:${type === "sonarr" ? "8989" : "7878"}`}
          required
        />
        <p className="text-xs text-muted-foreground">
          The base URL of your {type === "sonarr" ? "Sonarr" : "Radarr"} instance
        </p>
      </div>

      <div className="space-y-2">
        <Label htmlFor="apiKey">API Key {isEdit ? "(leave empty to keep current)" : "*"}</Label>
        <Input
          id="apiKey"
          type="password"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          placeholder={isEdit ? "••••••••" : "Enter API key"}
          required={!isEdit}
        />
        <p className="text-xs text-muted-foreground">
          Found in Settings &gt; General in {type === "sonarr" ? "Sonarr" : "Radarr"}
        </p>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <div className="space-y-2">
          <Label htmlFor="priority">Priority</Label>
          <Input
            id="priority"
            type="number"
            value={priority}
            onChange={(e) => setPriority(parseInt(e.target.value) || 0)}
            min={0}
          />
          <p className="text-xs text-muted-foreground">
            Higher priority instances are queried first
          </p>
        </div>

        <div className="space-y-2">
          <Label htmlFor="timeout">Timeout (seconds)</Label>
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
          Enable this instance
        </Label>
      </div>

      {testResult && (
        <div className={`text-sm p-2 rounded ${testResult.success ? "bg-green-500/10 text-green-500" : "bg-destructive/10 text-destructive"}`}>
          {testResult.success ? "Connection successful" : `Connection failed: ${testResult.error}`}
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
              Testing...
            </>
          ) : (
            <>
              <Zap className="mr-2 h-4 w-4" />
              Test Connection
            </>
          )}
        </Button>
        <div className="flex gap-2">
          <Button type="button" variant="outline" onClick={onCancel} disabled={isPending}>
            Cancel
          </Button>
          <Button type="submit" disabled={isPending}>
            {isPending ? "Saving..." : isEdit ? "Update" : "Create"}
          </Button>
        </div>
      </div>
    </form>
  )
}
