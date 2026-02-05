/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { AddTorrentDialog, type AddTorrentDropPayload } from "@/components/torrents/AddTorrentDialog"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useAuth } from "@/hooks/useAuth"
import { useInstances } from "@/hooks/useInstances"
import { clearAddIntent, storeAddIntent } from "@/lib/add-intent"
import { consumeLaunchQueueEvent, subscribeLaunchQueueEvents, type LaunchQueueEvent } from "@/lib/launch-queue"
import { normalizeMagnetLink } from "@/lib/magnet"
import type { InstanceResponse } from "@/types"
import { createFileRoute, Navigate, useNavigate } from "@tanstack/react-router"
import { Loader2 } from "lucide-react"
import { useCallback, useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import { z } from "zod"

const addSearchSchema = z.object({
  magnet: z.string().optional(),
  url: z.string().optional(),
  instance: z.coerce.number().optional(),
  expectingFiles: z.enum(["true", "false"]).optional(),
})

export const Route = createFileRoute("/add")({
  validateSearch: addSearchSchema,
  component: AddTorrentHandler,
})

function AddTorrentHandler() {
  const { isAuthenticated, isLoading: authLoading } = useAuth()
  const { magnet: magnetParam, url, instance, expectingFiles: expectingFilesParam } = Route.useSearch()
  const navigate = useNavigate()
  const { instances, isLoading: instancesLoading } = useInstances()

  const expectingFiles = expectingFilesParam === "true"
  const magnet = normalizeMagnetLink(magnetParam) ?? normalizeMagnetLink(url) ?? undefined

  const [selectedInstanceId, setSelectedInstanceId] = useState<number | null>(instance ?? null)
  const [dialogOpen, setDialogOpen] = useState(true)
  const [dropPayload, setDropPayload] = useState<AddTorrentDropPayload | null>(null)

  // Track if payload was ever received (prevents falling back to "waiting" state after consumption)
  const hasReceivedPayload = useRef(false)

  // Filter to active instances
  const activeInstances = instances?.filter(i => i.isActive) ?? []

  // Initialize payload from magnet URL param on mount
  useEffect(() => {
    if (magnet) {
      hasReceivedPayload.current = true
      setDropPayload({ type: "url", urls: [magnet] })
      return
    }

    const hasRawParam = (typeof magnetParam === "string" && magnetParam.trim().length > 0) ||
      (typeof url === "string" && url.trim().length > 0)
    if (hasRawParam) {
      hasReceivedPayload.current = true
      clearAddIntent()
      toast.error("Invalid magnet link")
      navigate({ to: "/" })
    }
  }, [magnet, magnetParam, navigate, url])

  const handleLaunchQueueEvent = useCallback((event: LaunchQueueEvent) => {
    if (event.kind === "payload") {
      hasReceivedPayload.current = true
      setDropPayload(event.payload.type === "file"
        ? { type: "file", files: event.payload.files }
        : { type: "url", urls: event.payload.urls })
      return
    }

    clearAddIntent()
    toast.error("No valid .torrent files found")
    navigate({ to: "/" })
  }, [navigate])

  // Handle files from file handler (launchQueue API)
  useEffect(() => {
    if (authLoading || !isAuthenticated || magnet) return

    const pendingEvent = consumeLaunchQueueEvent()
    if (pendingEvent) {
      handleLaunchQueueEvent(pendingEvent)
    }

    return subscribeLaunchQueueEvents(handleLaunchQueueEvent)
  }, [authLoading, handleLaunchQueueEvent, isAuthenticated, magnet])

  // Check if running as installed PWA (file-handler launches only happen in standalone mode)
  const isStandalone = window.matchMedia("(display-mode: standalone)").matches ||
    (navigator as Navigator & { standalone?: boolean }).standalone === true

  // Handle timeout when expecting files (from auth redirect or standalone mode file-handler)
  useEffect(() => {
    // Skip if we already have a payload or aren't expecting one
    if (hasReceivedPayload.current) return
    if (!expectingFiles && !isStandalone) return
    // If we have a magnet, no need to wait for files
    if (magnet) return

    const timeout = setTimeout(() => {
      if (!hasReceivedPayload.current) {
        // Only show error if we explicitly expected files (came from auth redirect)
        if (expectingFiles) {
          toast.error("Unable to open torrent file. Please try again.")
        }
        navigate({ to: "/" })
      }
    }, 2000)

    return () => clearTimeout(timeout)
  }, [expectingFiles, isStandalone, magnet, navigate])

  const handleCancel = useCallback(() => {
    navigate({ to: "/" })
  }, [navigate])

  const handlePayloadConsumed = useCallback(() => {
    setDropPayload(null)
  }, [])

  // Determine which instance to use
  const resolvedInstanceId = selectedInstanceId ??
    (activeInstances.length === 1 ? activeInstances[0].id : null)

  const handleOpenChange = useCallback((open: boolean) => {
    if (open) return
    setDialogOpen(false)
    if (resolvedInstanceId) {
      navigate({ to: "/instances/$instanceId", params: { instanceId: String(resolvedInstanceId) } })
    } else {
      navigate({ to: "/" })
    }
  }, [navigate, resolvedInstanceId])

  if (authLoading) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (!isAuthenticated) {
    // Store intent if there's a payload OR if we're in PWA mode (potential file-handler launch)
    if (magnet) {
      storeAddIntent({ magnet, openAdd: true })
    } else if (expectingFiles || isStandalone) {
      // In standalone mode without magnet, files may be coming via launchQueue
      storeAddIntent({ hasFiles: true, openAdd: true })
    }
    return <Navigate to="/login" />
  }

  // If authenticated but no payload and not expecting files via launchQueue, redirect home
  // In standalone mode, wait for potential file-handler payload
  if (!magnet && !expectingFiles && !isStandalone) {
    return <Navigate to="/" />
  }

  // Wait for instances to load
  if (instancesLoading) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  // If no instances configured, redirect to settings
  if (activeInstances.length === 0) {
    return <Navigate to="/settings" search={{ tab: "instances" }} />
  }

  // If multiple instances and none selected, show selector
  if (!resolvedInstanceId && activeInstances.length > 1) {
    return (
      <InstanceSelector
        instances={activeInstances}
        onSelect={setSelectedInstanceId}
        onCancel={handleCancel}
      />
    )
  }

  // If we have an instance and payload (or payload was already consumed), show dialog
  if (resolvedInstanceId && (dropPayload || hasReceivedPayload.current)) {
    return (
      <div className="flex h-screen items-center justify-center bg-background/80 backdrop-blur-sm p-4">
        <AddTorrentDialog
          instanceId={resolvedInstanceId}
          open={dialogOpen}
          onOpenChange={handleOpenChange}
          dropPayload={dropPayload}
          onDropPayloadConsumed={handlePayloadConsumed}
        />
      </div>
    )
  }

  // Waiting for launchQueue to provide payload
  if (resolvedInstanceId) {
    return (
      <div className="flex h-screen flex-col items-center justify-center gap-4 bg-background">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
        <p className="text-sm text-muted-foreground">Waiting for torrent data...</p>
      </div>
    )
  }

  // Fallback - shouldn't reach here
  return <Navigate to="/" />
}

interface InstanceSelectorProps {
  instances: InstanceResponse[]
  onSelect: (instanceId: number) => void
  onCancel: () => void
}

function InstanceSelector({ instances, onSelect, onCancel }: InstanceSelectorProps) {
  return (
    <div className="flex h-screen items-center justify-center bg-background p-4">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>Select Instance</CardTitle>
          <CardDescription>
            Choose which qBittorrent instance to add this torrent to
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          {instances.map((instance) => (
            <Button
              key={instance.id}
              variant="outline"
              className="w-full justify-start"
              onClick={() => onSelect(instance.id)}
            >
              <div className="flex items-center gap-2">
                <div className={`h-2 w-2 rounded-full ${instance.connected ? "bg-green-500" : "bg-red-500"
                  }`} />
                <span>{instance.name || `Instance ${instance.id}`}</span>
              </div>
            </Button>
          ))}
          <Button
            variant="ghost"
            className="w-full"
            onClick={onCancel}
          >
            Cancel
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}
