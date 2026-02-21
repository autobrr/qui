/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useLayoutRoute } from "@/contexts/LayoutRouteContext"
import { ALL_INSTANCES_ID } from "@/lib/instances"
import { Torrents } from "@/pages/Torrents"
import { createFileRoute } from "@tanstack/react-router"
import { useLayoutEffect } from "react"
import { z } from "zod"

const unifiedSearchSchema = z.object({
  modal: z.enum(["add-torrent", "create-torrent", "tasks"]).optional(),
  torrent: z.string().optional(),
  tab: z.string().optional(),
  instanceIds: z.string().optional(),
})

export const Route = createFileRoute("/_authenticated/instances/")({
  validateSearch: unifiedSearchSchema,
  component: UnifiedInstanceTorrents,
  staticData: {
    title: "Unified",
  },
})

function UnifiedInstanceTorrents() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const { setLayoutRouteState, resetLayoutRouteState } = useLayoutRoute()

  useLayoutEffect(() => {
    setLayoutRouteState({
      showInstanceControls: true,
      instanceId: ALL_INSTANCES_ID,
    })

    return () => {
      resetLayoutRouteState()
    }
  }, [resetLayoutRouteState, setLayoutRouteState])

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

  return (
    <Torrents
      instanceId={ALL_INSTANCES_ID}
      instanceName="Unified"
      isAllInstancesView
      search={search}
      onSearchChange={handleSearchChange}
    />
  )
}
