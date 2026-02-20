/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { useLayoutRoute } from "@/contexts/LayoutRouteContext"
import { useInstances } from "@/hooks/useInstances"
import { Torrents } from "@/pages/Torrents"
import { createFileRoute, Navigate } from "@tanstack/react-router"
import { Power } from "lucide-react"
import { useLayoutEffect } from "react"
import { z } from "zod"

const instanceSearchSchema = z.object({
  modal: z.enum(["add-torrent", "create-torrent", "tasks"]).optional(),
  torrent: z.string().optional(),
  tab: z.string().optional(),
  instanceIds: z.string().optional(),
})

export const Route = createFileRoute("/_authenticated/instances/$instanceId")({
  validateSearch: instanceSearchSchema,
  component: InstanceTorrents,
  staticData: {
    title: "Torrents",
  },
})

function InstanceTorrents() {
  const { instanceId } = Route.useParams()
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const { setLayoutRouteState, resetLayoutRouteState } = useLayoutRoute()
  const { instances, isLoading } = useInstances()
  const instanceIdNumber = Number.parseInt(instanceId, 10)
  const instance = instances?.find(i => i.id === instanceIdNumber)
  const shouldShowInstanceControls = instance?.isActive ?? false

  useLayoutEffect(() => {
    if (!Number.isFinite(instanceIdNumber)) {
      resetLayoutRouteState()
      return
    }

    setLayoutRouteState({
      showInstanceControls: shouldShowInstanceControls,
      instanceId: instanceIdNumber,
    })

    return () => {
      resetLayoutRouteState()
    }
  }, [instanceIdNumber, resetLayoutRouteState, setLayoutRouteState, shouldShowInstanceControls])

  const handleSearchChange = (newSearch: {
    modal?: "add-torrent" | "create-torrent" | "tasks" | undefined
    torrent?: string
    tab?: string
    instanceIds?: string
  }) => {
    navigate({
      search: newSearch,
      replace: true,
    })
  }

  if (isLoading) {
    return <div className="p-6">Loading instances...</div>
  }

  if (!instance) {
    return (
      <div className="p-6">
        <h1>Instance not found</h1>
        <p>Instance ID: {instanceId}</p>
        <p>Available instances: {instances?.map(i => i.id).join(", ")}</p>
        <Navigate to="/settings" search={{ tab: "instances" }} />
      </div>
    )
  }

  const instanceDisplayName = instance.name?.trim() || `Instance ${instance.id}`

  if (!instance.isActive) {
    const handleManageInstances = () => {
      navigate({ to: "/settings", search: { tab: "instances" as const } })
    }

    return (
      <InstanceDisabledNotice
        instanceName={instanceDisplayName}
        onManageInstances={handleManageInstances}
      />
    )
  }

  return (
    <Torrents
      instanceId={instanceIdNumber}
      instanceName={instanceDisplayName}
      search={search}
      onSearchChange={handleSearchChange}
    />
  )
}

interface InstanceDisabledNoticeProps {
  instanceName: string
  onManageInstances: () => void
}

function InstanceDisabledNotice({ instanceName, onManageInstances }: InstanceDisabledNoticeProps) {
  return (
    <div className="flex h-full items-center justify-center px-4 py-12">
      <div className="max-w-xl text-center space-y-4">
        <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-muted">
          <Power className="h-7 w-7 text-muted-foreground" aria-hidden="true" />
        </div>
        <div className="space-y-2">
          <h1 className="text-2xl font-semibold">Instance disabled</h1>
          <p className="text-muted-foreground">
            {instanceName} is currently disabled. Enable it from Settings &gt; Instances to resume torrent management.
          </p>
        </div>
        <Button onClick={onManageInstances} size="sm">
          Manage Instances
        </Button>
      </div>
    </div>
  )
}
