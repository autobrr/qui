/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createFileRoute, Navigate } from "@tanstack/react-router"
import { Torrents } from "@/pages/Torrents"
import { useInstances } from "@/hooks/useInstances"
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
  const { instances, isLoading } = useInstances()

  const handleSearchChange = (newSearch: { modal?: "add-torrent" | "create-torrent" | "tasks" | undefined }) => {
    navigate({
      search: newSearch,
      replace: true,
    })
  }

  if (isLoading) {
    return <div>{t("pages.instance.loading")}</div>
  }

  const instance = instances?.find(i => i.id === parseInt(instanceId))

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