/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"

import { useDebounce } from "@/hooks/useDebounce"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities.ts"
import { api } from "@/lib/api"
import { formatBytes } from "@/lib/utils"

interface DestinationFreeSpaceProps {
  instanceId: number
  path: string
}

/**
 * Shows the free space qBittorrent reports for the selected destination. The query
 * key carries the path, so a late answer for an earlier destination lands in its own
 * cache entry instead of overwriting the current one.
 */
export function DestinationFreeSpace({ instanceId, path }: DestinationFreeSpaceProps) {
  const { t } = useTranslation("torrents")
  const { data: capabilities } = useInstanceCapabilities(instanceId)
  // Undefined while the capabilities query is in flight, which is not the same as unsupported.
  const supported = capabilities?.supportsFreeSpaceAtPath
  const debouncedPath = useDebounce(path.trim(), 400)

  const { data, isPending } = useQuery({
    queryKey: ["instance-free-space", instanceId, debouncedPath],
    queryFn: () => api.getFreeSpaceAtPath(instanceId, debouncedPath),
    enabled: supported === true && debouncedPath !== "",
    staleTime: 30_000,
    retry: false,
  })

  // Older instances have no path endpoint, so they keep the default-path display only.
  if (supported === false) {
    return null
  }

  const bytes = data?.bytes ?? null
  const pending = supported !== true || (debouncedPath !== "" && isPending)

  return (
    <p className="text-xs text-muted-foreground">
      {t("addTorrentDialog.options.freeSpaceAtDestination", {
        value: pending ? "..." : bytes === null ? t("common:status.unavailable") : formatBytes(bytes),
      })}
    </p>
  )
}
