/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from "react"

import { type PageTitle, publishPageTitle } from "@/lib/page-title"

export function usePageTitle({ detail, speeds }: PageTitle) {
  useEffect(() => {
    publishPageTitle({ detail, speeds })
    return () => publishPageTitle({})
  }, [detail, speeds])
}
