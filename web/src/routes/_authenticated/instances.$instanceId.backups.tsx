/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useInstances } from "@/hooks/useInstances"
import { InstanceBackups } from "@/pages/InstanceBackups"
import { createFileRoute, Navigate } from "@tanstack/react-router"

export const Route = createFileRoute("/_authenticated/instances/$instanceId/backups")({
  component: InstanceBackupsRoute,
})

function InstanceBackupsRoute() {
  const { instanceId } = Route.useParams()
  const { instances, isLoading } = useInstances()

  if (isLoading) {
    return <div className="p-6">Loading instances...</div>
  }

  const instanceIdNumber = parseInt(instanceId)
  const instance = instances?.find(i => i.id === instanceIdNumber)

  if (!instance) {
    return (
      <div className="p-6">
        <h1>Instance not found</h1>
        <p>Instance ID: {instanceId}</p>
        <Navigate to="/instances" />
      </div>
    )
  }

  return <InstanceBackups instanceId={instanceIdNumber} />
}

