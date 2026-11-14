/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { useState } from "react"

import { api } from "@/lib/api"
import type { SystemNotice } from "@/types"

export const systemNoticesQueryKey = ["system-notices"]

const noNotices: SystemNotice[] = []

// useSystemNotices returns the open System notices. qui writes the Transfer
// history each 5 minutes, so a check each minute is often enough.
export function useSystemNotices(): SystemNotice[] {
  const { data } = useQuery({
    queryKey: systemNoticesQueryKey,
    queryFn: () => api.getSystemNotices(),
    refetchInterval: 60 * 1000,
  })
  return data ?? noNotices
}

// useOpenSystemNotice holds the notice whose detail a menu opened. It forgets
// the notice when the server closes it, so that the same kind opening again
// later does not open its detail with no click.
export function useOpenSystemNotice(notices: SystemNotice[]) {
  const [openKind, setOpenKind] = useState<string>()
  const notice = notices.find((candidate) => candidate.kind === openKind)
  if (openKind !== undefined && notice === undefined) {
    setOpenKind(undefined)
  }
  return { notice, open: setOpenKind, close: () => setOpenKind(undefined) }
}
