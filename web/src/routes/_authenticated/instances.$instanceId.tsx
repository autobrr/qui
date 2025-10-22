/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useLayoutRoute } from "@/contexts/LayoutRouteContext"
import { useInstances } from "@/hooks/useInstances"
import { Torrents } from "@/pages/Torrents"
import { createFileRoute, Navigate } from "@tanstack/react-router"
import { useLayoutEffect } from "react"
import { z } from "zod"
import { useTranslation } from "react-i18next"

const instanceSearchSchema = z.object({
  modal: z.enum(["add-torrent", "create-torrent", "tasks"]).optional(),
})

export const Route = createFileRoute("/_authenticated/instances/$instanceId")({
  validateSearch: instanceSearchSchema,
  component: InstanceTorrents,
})

function InstanceTorrents() {
  const { t } = useTranslation()
  const { instanceId } = Route.useParams()
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
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

  const handleSearchChange = (newSearch: { modal?: "add-torrent" | "create-torrent" | "tasks" | undefined }) => {
    navigate({
      search: newSearch,
      replace: true,
    })
  }

  if (isLoading) {
    return <div className="p-6">{t("pages.instance.loading")}</div>
  }

  const instance = instances?.find(i => i.id === instanceIdNumber)

  if (!instance) {
    return (
      <div className="p-6">
        <h1>{t("pages.instance.notFound")}</h1>
        <p>
          {t("pages.instance.id")} {instanceId}
        </p>
        <p>
          {t("pages.instance.available")} {instances?.map(i => i.id).join(", ")}
        </p>
        <Navigate to="/settings" search={{ tab: "instances" }} />
      </div>
    )
  }

  return (
    <Torrents
      instanceId={instanceIdNumber}
      instanceName={instance.name}
      search={search}
      onSearchChange={handleSearchChange}
    />
  )
}