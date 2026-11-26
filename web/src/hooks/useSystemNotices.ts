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

export function useSystemNotices(): SystemNotice[] {
  const { data } = useQuery({
    queryKey: systemNoticesQueryKey,
    queryFn: () => api.getSystemNotices(),
    refetchInterval: 60 * 1000,
  })
  return data ?? noNotices
}

export function useOpenSystemNotice(notices: SystemNotice[]) {
  const [openKind, setOpenKind] = useState<string>()
  const [menuButton, setMenuButton] = useState<HTMLElement | null>(null)
  const notice = notices.find((candidate) => candidate.kind === openKind)
  if (openKind !== undefined && notice === undefined) {
    setOpenKind(undefined)
  }
  return {
    notice,
    menuButton,
    open: (kind: string, opener: HTMLElement | null) => {
      setOpenKind(kind)
      setMenuButton(opener)
    },
    close: () => setOpenKind(undefined),
  }
}
