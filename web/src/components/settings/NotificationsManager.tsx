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
import { Checkbox } from "@/components/ui/checkbox"
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion"
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
import { Switch } from "@/components/ui/switch"
import { api } from "@/lib/api"
import type { NotificationEventDefinition, NotificationTarget, NotificationTargetRequest } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Bell, Edit, Loader2, Plus, Send, Trash2 } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

const getErrorMessage = (error: unknown, fallback: string) => {
  if (error instanceof Error && error.message) {
    return error.message
  }
  if (typeof error === "string" && error.trim()) {
    return error
  }
  return fallback
}

interface NotificationTargetFormProps {
  initial?: NotificationTarget | null
  eventDefinitions: NotificationEventDefinition[]
  onSubmit: (data: NotificationTargetRequest) => void
  onCancel: () => void
  isPending: boolean
}

function NotificationTargetForm({ initial, eventDefinitions, onSubmit, onCancel, isPending }: NotificationTargetFormProps) {
  const [name, setName] = useState(initial?.name ?? "")
  const [url, setUrl] = useState(initial?.url ?? "")
  const [enabled, setEnabled] = useState(initial?.enabled ?? true)
  const [eventTypes, setEventTypes] = useState<string[]>(initial?.eventTypes ?? [])
  const [initialized, setInitialized] = useState(false)

  useEffect(() => {
    if (initialized) return
    if (initial) {
      setEventTypes(initial.eventTypes ?? [])
    } else if (eventDefinitions.length > 0) {
      setEventTypes(eventDefinitions.map((event) => event.type))
    }
    setInitialized(true)
  }, [eventDefinitions, initial, initialized])

  const toggleEvent = (type: string) => {
    setEventTypes((prev) =>
      prev.includes(type) ? prev.filter((eventType) => eventType !== type) : [...prev, type]
    )
  }

  const selectGroupEvents = (events: NotificationEventDefinition[]) => {
    setEventTypes((prev) => {
      const next = new Set(prev)
      for (const event of events) {
        next.add(event.type)
      }
      return Array.from(next)
    })
  }

  const clearGroupEvents = (events: NotificationEventDefinition[]) => {
    setEventTypes((prev) => {
      const blocked = new Set(events.map((event) => event.type))
      return prev.filter((eventType) => !blocked.has(eventType))
    })
  }

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()

    const trimmedName = name.trim()
    const trimmedUrl = url.trim()

    if (!trimmedName) {
      toast.error("Name is required")
      return
    }
    if (!trimmedUrl) {
      toast.error("URL is required")
      return
    }
    if (eventTypes.length === 0) {
      toast.error("Select at least one event")
      return
    }

    onSubmit({
      name: trimmedName,
      url: trimmedUrl,
      enabled,
      eventTypes,
    })
  }

  const allSelected = eventDefinitions.length > 0 && eventTypes.length === eventDefinitions.length
  const groupedEvents = useMemo(() => {
    const groups = new Map<string, NotificationEventDefinition[]>()
    const addToGroup = (label: string, event: NotificationEventDefinition) => {
      const existing = groups.get(label)
      if (existing) {
        existing.push(event)
      } else {
        groups.set(label, [event])
      }
    }

    for (const event of eventDefinitions) {
      if (event.type === "torrent_completed") {
        addToGroup("Torrent", event)
      } else if (
        event.type === "backup_succeeded" ||
        event.type === "backup_failed" ||
        event.type === "dir_scan_completed" ||
        event.type === "dir_scan_failed" ||
        event.type === "orphan_scan_completed" ||
        event.type === "orphan_scan_failed"
      ) {
        addToGroup("Maintenance", event)
      } else if (event.type.startsWith("cross_seed_")) {
        addToGroup("Cross-seed", event)
      } else if (event.type.startsWith("automations_")) {
        addToGroup("Automations", event)
      } else {
        addToGroup("Other", event)
      }
    }

    const ordered = ["Torrent", "Maintenance", "Cross-seed", "Automations", "Other"]
    return ordered
      .map((label) => ({ label, events: groups.get(label) ?? [] }))
      .filter((group) => group.events.length > 0)
  }, [eventDefinitions])

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="notification-name">Name</Label>
        <Input
          id="notification-name"
          placeholder="My Discord"
          value={name}
          onChange={(e) => setName(e.target.value)}
          data-1p-ignore
          autoComplete="off"
        />
      </div>

      <div className="space-y-2">
        <Label htmlFor="notification-url">Shoutrrr URL</Label>
        <Input
          id="notification-url"
          placeholder="discord://token@channel or notifiarr://apikey"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
        />
        <p className="text-xs text-muted-foreground">
          Use any Shoutrrr-supported URL scheme. Notifiarr uses <span className="font-mono">notifiarr://apikey</span>.
        </p>
      </div>

      <div className="flex items-center justify-between rounded-md border px-3 py-2">
        <div>
          <Label className="text-sm">Enabled</Label>
          <p className="text-xs text-muted-foreground">Toggle delivery for this target.</p>
        </div>
        <Switch checked={enabled} onCheckedChange={setEnabled} />
      </div>

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <Label className="text-sm">Events</Label>
          <div className="flex gap-2">
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => setEventTypes(eventDefinitions.map((event) => event.type))}
              disabled={eventDefinitions.length === 0 || allSelected}
            >
              Select all
            </Button>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => setEventTypes([])}
              disabled={eventTypes.length === 0}
            >
              Clear
            </Button>
          </div>
        </div>
        <div className="space-y-4 rounded-md border p-3">
          {eventDefinitions.length === 0 && (
            <p className="text-sm text-muted-foreground">Loading event types…</p>
          )}
          <Accordion type="multiple" className="space-y-2">
            {groupedEvents.map((group) => {
              const groupTypes = group.events.map((event) => event.type)
              const groupSelected = groupTypes.filter((type) => eventTypes.includes(type))
              const allGroupSelected = groupSelected.length === groupTypes.length
              const anyGroupSelected = groupSelected.length > 0
              return (
                <AccordionItem
                  key={group.label}
                  value={group.label}
                  className="rounded-md border last:!border-b"
                >
                  <AccordionTrigger className="px-3 py-2 text-sm hover:no-underline">
                    <div className="flex flex-1 items-center justify-between gap-3">
                      <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                        {group.label}
                      </span>
                      <div className="flex items-center gap-2">
                        <Button
                          type="button"
                          size="xs"
                          variant="outline"
                          onClick={(event) => {
                            event.preventDefault()
                            event.stopPropagation()
                            selectGroupEvents(group.events)
                          }}
                          disabled={group.events.length === 0 || allGroupSelected}
                        >
                          Select
                        </Button>
                        <Button
                          type="button"
                          size="xs"
                          variant="outline"
                          onClick={(event) => {
                            event.preventDefault()
                            event.stopPropagation()
                            clearGroupEvents(group.events)
                          }}
                          disabled={!anyGroupSelected}
                        >
                          Clear
                        </Button>
                      </div>
                    </div>
                  </AccordionTrigger>
                  <AccordionContent className="space-y-3 px-3 pb-3">
                    {group.events.map((event) => (
                      <label key={event.type} className="flex items-start gap-3 text-sm">
                        <Checkbox
                          checked={eventTypes.includes(event.type)}
                          onCheckedChange={() => toggleEvent(event.type)}
                        />
                        <span className="space-y-1">
                          <span className="font-medium text-foreground">{event.label}</span>
                          <span className="block text-xs text-muted-foreground">{event.description}</span>
                        </span>
                      </label>
                    ))}
                  </AccordionContent>
                </AccordionItem>
              )
            })}
          </Accordion>
        </div>
      </div>

      <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <Button type="button" variant="outline" onClick={onCancel} disabled={isPending}>
          Cancel
        </Button>
        <Button type="submit" disabled={isPending}>
          {isPending ? (
            <>
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              Saving…
            </>
          ) : (
            "Save"
          )}
        </Button>
      </div>
    </form>
  )
}

export function NotificationsManager() {
  const queryClient = useQueryClient()
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [editTarget, setEditTarget] = useState<NotificationTarget | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<NotificationTarget | null>(null)

  const { data: eventDefinitions = [] } = useQuery({
    queryKey: ["notificationEvents"],
    queryFn: () => api.listNotificationEvents(),
    staleTime: 5 * 60 * 1000,
  })

  const { data: targets, isLoading, error } = useQuery({
    queryKey: ["notificationTargets"],
    queryFn: () => api.listNotificationTargets(),
    staleTime: 30 * 1000,
  })

  const eventLabelMap = useMemo(() => {
    const map = new Map<string, string>()
    for (const event of eventDefinitions) {
      map.set(event.type, event.label)
    }
    return map
  }, [eventDefinitions])

  const createMutation = useMutation({
    mutationFn: (data: NotificationTargetRequest) => api.createNotificationTarget(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["notificationTargets"] })
      setShowCreateDialog(false)
      toast.success("Notification target created")
    },
    onError: (error: unknown) => {
      toast.error(getErrorMessage(error, "Failed to create notification target"))
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: NotificationTargetRequest }) => api.updateNotificationTarget(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["notificationTargets"] })
      setEditTarget(null)
      toast.success("Notification target updated")
    },
    onError: (error: unknown) => {
      toast.error(getErrorMessage(error, "Failed to update notification target"))
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => api.deleteNotificationTarget(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["notificationTargets"] })
      setDeleteTarget(null)
      toast.success("Notification target deleted")
    },
    onError: (error: unknown) => {
      toast.error(getErrorMessage(error, "Failed to delete notification target"))
    },
  })

  const testMutation = useMutation({
    mutationFn: (id: number) => api.testNotificationTarget(id),
    onSuccess: () => {
      toast.success("Test notification sent")
    },
    onError: (error: unknown) => {
      toast.error(getErrorMessage(error, "Failed to send test notification"))
    },
  })

  const formatEventLabel = (type: string) => eventLabelMap.get(type) ?? type

  const renderEventBadges = (events: string[]) => {
    if (events.length === 0) {
      return <Badge variant="secondary">All events</Badge>
    }
    return (
      <div className="flex flex-wrap gap-2">
        {events.map((event) => (
          <Badge key={event} variant="outline">
            {formatEventLabel(event)}
          </Badge>
        ))}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-col items-stretch gap-2 sm:flex-row sm:justify-end">
        <Dialog open={showCreateDialog} onOpenChange={setShowCreateDialog}>
          <DialogTrigger asChild>
            <Button size="sm" className="w-full sm:w-auto">
              <Plus className="mr-2 h-4 w-4" />
              Add Notification Target
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-2xl max-w-full max-h-[90dvh] flex flex-col">
            <DialogHeader>
              <DialogTitle>New Notification Target</DialogTitle>
              <DialogDescription>
                Configure where qui should send alerts and status updates.
              </DialogDescription>
            </DialogHeader>
            <div className="flex-1 overflow-y-auto min-h-0">
              <NotificationTargetForm
                eventDefinitions={eventDefinitions}
                onSubmit={(data) => createMutation.mutate(data)}
                onCancel={() => setShowCreateDialog(false)}
                isPending={createMutation.isPending}
              />
            </div>
          </DialogContent>
        </Dialog>
      </div>

      {isLoading && <div className="text-center py-8">Loading notification targets…</div>}
      {error && (
        <Card>
          <CardContent className="pt-6">
            <div className="text-destructive">Failed to load notification targets</div>
          </CardContent>
        </Card>
      )}

      {!isLoading && !error && (!targets || targets.length === 0) && (
        <Card>
          <CardContent className="pt-6">
            <div className="text-center text-muted-foreground">
              No notification targets configured. Add one to start receiving alerts.
            </div>
          </CardContent>
        </Card>
      )}

      {targets && targets.length > 0 && (
        <div className="grid gap-4">
          {targets.map((target) => (
            <Card className="bg-muted/40" key={target.id}>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="space-y-1 flex-1">
                    <div className="flex items-center gap-2">
                      <CardTitle className="text-lg flex items-center gap-2">
                        <Bell className="h-4 w-4" />
                        {target.name}
                      </CardTitle>
                      <Badge variant={target.enabled ? "default" : "secondary"}>
                        {target.enabled ? "Enabled" : "Disabled"}
                      </Badge>
                    </div>
                    <CardDescription className="text-xs break-all">
                      {target.url}
                    </CardDescription>
                  </div>
                  <div className="flex gap-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => testMutation.mutate(target.id)}
                      aria-label={`Send test to ${target.name}`}
                      disabled={testMutation.isPending}
                    >
                      <Send className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setEditTarget(target)}
                      aria-label={`Edit ${target.name}`}
                    >
                      <Edit className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setDeleteTarget(target)}
                      aria-label={`Delete ${target.name}`}
                    >
                      <Trash2 className="h-4 w-4 text-destructive" />
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent className="space-y-2 text-sm">
                <div>
                  <p className="text-muted-foreground text-xs mb-2">Events</p>
                  {renderEventBadges(target.eventTypes)}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Dialog open={!!editTarget} onOpenChange={(open) => !open && setEditTarget(null)}>
        <DialogContent className="sm:max-w-2xl max-w-full max-h-[90dvh] flex flex-col">
          <DialogHeader>
            <DialogTitle>Edit Notification Target</DialogTitle>
            <DialogDescription>Update delivery settings for this target.</DialogDescription>
          </DialogHeader>
          <div className="flex-1 overflow-y-auto min-h-0">
            {editTarget && (
              <NotificationTargetForm
                initial={editTarget}
                eventDefinitions={eventDefinitions}
                onSubmit={(data) => updateMutation.mutate({ id: editTarget.id, data })}
                onCancel={() => setEditTarget(null)}
                isPending={updateMutation.isPending}
              />
            )}
          </div>
        </DialogContent>
      </Dialog>

      <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete notification target?</AlertDialogTitle>
            <AlertDialogDescription>
              This will remove {deleteTarget?.name}. You can re-add it later if needed.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
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
