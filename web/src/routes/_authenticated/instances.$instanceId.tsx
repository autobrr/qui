/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createFileRoute, Navigate } from '@tanstack/react-router'
import { Torrents } from '@/pages/Torrents'
import { useInstances } from '@/hooks/useInstances'

export const Route = createFileRoute('/_authenticated/instances/$instanceId')({
  component: InstanceTorrents,
})

function InstanceTorrents() {
  const { instanceId } = Route.useParams()
  const { instances, isLoading } = useInstances()
  
  if (isLoading) {
    return <div>Loading instances...</div>
  }
  
  const instance = instances?.find(i => i.id === parseInt(instanceId))
  
  if (!instance) {
    return (
      <div className="p-6">
        <h1>Instance not found</h1>
        <p>Instance ID: {instanceId}</p>
        <p>Available instances: {instances?.map(i => i.id).join(', ')}</p>
        <Navigate to="/instances" />
      </div>
    )
  }
  
  return <Torrents instanceId={parseInt(instanceId)} instanceName={instance.name} />
}