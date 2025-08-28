/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createFileRoute, Navigate } from "@tanstack/react-router"
import { Torrents } from "@/pages/Torrents"
import { useInstances } from "@/hooks/useInstances"
import { z } from "zod"

const instanceSearchSchema = z.object({
  modal: z.enum(["add-torrent"]).optional(),
})

export const Route = createFileRoute("/_authenticated/instances/$instanceId")({
  validateSearch: instanceSearchSchema,
  component: InstanceTorrents,
})

function InstanceTorrents() {
  const { instanceId } = Route.useParams()
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const { instances, isLoading } = useInstances()
  
  const handleSearchChange = (newSearch: { modal?: "add-torrent" | undefined }) => {
    navigate({
      search: newSearch,
      replace: true,
    })
  }
  
  if (isLoading) {
    return <div>Loading instances...</div>
  }
  
  const instance = instances?.find(i => i.id === parseInt(instanceId))
  
  if (!instance) {
    return (
      <div className="p-6">
        <h1>Instance not found</h1>
        <p>Instance ID: {instanceId}</p>
        <p>Available instances: {instances?.map(i => i.id).join(", ")}</p>
        <Navigate to="/instances" />
      </div>
    )
  }
  
  return (
    <Torrents 
      instanceId={parseInt(instanceId)} 
      instanceName={instance.name} 
      search={search}
      onSearchChange={handleSearchChange}
    />
  )
}