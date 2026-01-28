/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQueries, useQuery } from "@tanstack/react-query"
import { useLocation } from "@tanstack/react-router"
import { useEffect, useMemo, useRef } from "react"

import { api } from "@/lib/api"
import { useIncognitoMode } from "@/lib/incognito"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import type { InstanceResponse, TorrentResponse } from "@/types"

const DEFAULT_TITLE = "qui"

export function useTransferTitle() {
  const baseTitleRef = useRef<string>(
    typeof document !== "undefined" && document.title ? document.title : DEFAULT_TITLE
  )
  const [speedUnit] = useSpeedUnits()
  const [incognito] = useIncognitoMode()
  const location = useLocation()

  const isAuthRoute =
    location.pathname.startsWith("/login") ||
    location.pathname.startsWith("/setup")
  const shouldFetch = !incognito && !isAuthRoute

  const { data: instances = [] } = useQuery<InstanceResponse[]>({
    queryKey: ["instances"],
    queryFn: () => api.getInstances(),
    refetchInterval: 30_000,
    refetchIntervalInBackground: true,
    staleTime: 30_000,
    enabled: shouldFetch,
  })

  const torrentsQueries = useQueries({
    queries: instances.map(instance => ({
      queryKey: ["torrents-list", instance.id, 0, undefined, undefined, "added_on", "desc"],
      queryFn: () => api.getTorrents(instance.id, {
        page: 0,
        limit: 1,
        sort: "added_on",
        order: "desc" as const,
      }),
      enabled: shouldFetch,
      refetchInterval: 5000,
      refetchIntervalInBackground: true,
      staleTime: 2000,
      gcTime: 300_000,
      placeholderData: (previousData: TorrentResponse | undefined) => previousData,
      retry: 1,
      retryDelay: 1000,
    })),
  })

  const totals = useMemo(() => {
    if (!shouldFetch) {
      return { download: 0, upload: 0 }
    }

    return torrentsQueries.reduce(
      (acc, query) => {
        const stats = (query.data as TorrentResponse | undefined)?.stats
        if (stats) {
          acc.download += stats.totalDownloadSpeed || 0
          acc.upload += stats.totalUploadSpeed || 0
        }
        return acc
      },
      { download: 0, upload: 0 }
    )
  }, [torrentsQueries, shouldFetch])

  useEffect(() => {
    if (typeof document === "undefined") return

    const baseTitle = baseTitleRef.current || DEFAULT_TITLE

    if (!shouldFetch) {
      document.title = baseTitle
      return
    }

    const download = formatSpeedWithUnit(totals.download, speedUnit)
    const upload = formatSpeedWithUnit(totals.upload, speedUnit)

    document.title = `[D: ${download}, U: ${upload}] ${baseTitle}`
  }, [totals.download, totals.upload, speedUnit, shouldFetch])
}
