/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useLayoutRoute } from "@/contexts/LayoutRouteContext"
import { useInstances } from "@/hooks/useInstances"
import { Titles } from "@/pages/Titles"
import { createFileRoute, Navigate } from "@tanstack/react-router"
import { useLayoutEffect } from "react"

export const Route = createFileRoute("/_authenticated/instances/$instanceId/titles")({
  component: InstanceTitles,
})

function InstanceTitles() {
  const { instanceId } = Route.useParams()
  const { setLayoutRouteState, resetLayoutRouteState } = useLayoutRoute()
  const { instances, isLoading } = useInstances()
  const instanceIdNumber = Number.parseInt(instanceId, 10)

  useLayoutEffect(() => {
    if (!Number.isFinite(instanceIdNumber)) {
      resetLayoutRouteState()
      return
    }

    setLayoutRouteState({
      showInstanceControls: true,
      instanceId: instanceIdNumber,
    })

    return () => {
      resetLayoutRouteState()
    }
  }, [instanceIdNumber, resetLayoutRouteState, setLayoutRouteState])

  if (isLoading) {
    return <div className="p-6">Loading instances...</div>
  }

  const instance = instances?.find(i => i.id === instanceIdNumber)

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

  return (
    <Titles
      instanceId={instanceIdNumber}
      instanceName={instance.name}
    />
  )
}
