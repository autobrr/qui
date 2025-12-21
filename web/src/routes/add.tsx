/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { AddTorrentDialog, type AddTorrentDropPayload } from "@/components/torrents/AddTorrentDialog"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useAuth } from "@/hooks/useAuth"
import { useInstances } from "@/hooks/useInstances"
import { storeAddIntent } from "@/lib/add-intent"
import type { InstanceResponse } from "@/types"
import { createFileRoute, Navigate, useNavigate } from "@tanstack/react-router"
import { Loader2 } from "lucide-react"
import { useCallback, useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import { z } from "zod"

const addSearchSchema = z.object({
  magnet: z.string().regex(/^magnet:\?/i).optional(),
  instance: z.coerce.number().optional(),
  expectingFiles: z.boolean().optional(),
})

export const Route = createFileRoute("/add")({
  validateSearch: addSearchSchema,
  component: AddTorrentHandler,
})

function AddTorrentHandler() {
  const { isAuthenticated, isLoading: authLoading } = useAuth()
  const { magnet, instance, expectingFiles } = Route.useSearch()
  const navigate = useNavigate()
  const { instances, isLoading: instancesLoading } = useInstances()

  const [selectedInstanceId, setSelectedInstanceId] = useState<number | null>(instance ?? null)
  const [dialogOpen, setDialogOpen] = useState(true)
  const [dropPayload, setDropPayload] = useState<AddTorrentDropPayload | null>(null)

  // Track if payload was ever received (prevents falling back to "waiting" state after consumption)
  const hasReceivedPayload = useRef(false)

  // Initialize payload from magnet URL param on mount
  useEffect(() => {
    if (magnet) {
      hasReceivedPayload.current = true
      setDropPayload({ type: "url", urls: [magnet] })
    }
  }, [magnet])

  // Handle files from file handler (launchQueue API)
  useEffect(() => {
    if (!window.launchQueue) return

    window.launchQueue.setConsumer(async (launchParams) => {
      if (!launchParams.files?.length) return

      const files: File[] = []
      for (const handle of launchParams.files) {
        try {
          const file = await handle.getFile()
          if (file.name.toLowerCase().endsWith(".torrent")) {
            files.push(file)
          }
        } catch (err) {
          console.error("Failed to get file from handle:", err)
        }
      }

      if (files.length > 0) {
        hasReceivedPayload.current = true
        setDropPayload({ type: "file", files })
      } else if (launchParams.files.length > 0) {
        // Files were provided but none were valid .torrent files
        toast.error("No valid .torrent files found")
        navigate({ to: "/" })
      }
    })
  }, [navigate])

  // Show error if we're expecting files from auth redirect but launchQueue doesn't fire
  useEffect(() => {
    if (!expectingFiles || hasReceivedPayload.current) return

    const timeout = setTimeout(() => {
      if (!hasReceivedPayload.current) {
        toast.error("Unable to open torrent file. Please try again.")
        navigate({ to: "/" })
      }
    }, 2000)

    return () => clearTimeout(timeout)
  }, [expectingFiles, navigate])

  const handleOpenChange = useCallback((open: boolean) => {
    if (!open) {
      setDialogOpen(false)
      if (selectedInstanceId) {
        navigate({ to: "/instances/$instanceId", params: { instanceId: String(selectedInstanceId) } })
      } else {
        navigate({ to: "/" })
      }
    }
  }, [navigate, selectedInstanceId])

  const handleCancel = useCallback(() => {
    navigate({ to: "/" })
  }, [navigate])

  const handlePayloadConsumed = useCallback(() => {
    setDropPayload(null)
  }, [])

  if (authLoading) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (!isAuthenticated) {
    storeAddIntent({ magnet, openAdd: true })
    return <Navigate to="/login" />
  }

  // Wait for instances to load
  if (instancesLoading) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  // Filter to active instances
  const activeInstances = instances?.filter(i => i.isActive) ?? []

  // If no instances configured, redirect to settings
  if (activeInstances.length === 0) {
    return <Navigate to="/settings" search={{ tab: "instances" }} />
  }

  // Determine which instance to use
  const resolvedInstanceId = selectedInstanceId ??
    (activeInstances.length === 1 ? activeInstances[0].id : null)

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
                <div className={`h-2 w-2 rounded-full ${
                  instance.connected ? "bg-green-500" : "bg-red-500"
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
