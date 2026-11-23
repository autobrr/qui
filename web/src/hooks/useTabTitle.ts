/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useRouterState } from "@tanstack/react-router"
import { useEffect, useSyncExternalStore } from "react"
import { useTranslation } from "react-i18next"

import type { namespaces } from "@/i18n"
import { readPageTitle, subscribePageTitle } from "@/lib/page-title"
import { useSpreadsheetDocumentTitle } from "@/lib/spreadsheet-disguise"

const APP_NAME = "qui"
const SEPARATOR = " | "

export interface RouteTitle {
  titleKey: string
  titleNs: (typeof namespaces)[number]
}

declare module "@tanstack/react-router" {
  interface StaticDataRouteOption {
    title: RouteTitle | null
  }
}

export function useTabTitle() {
  const { t } = useTranslation()
  const routeTitle = useRouterState({
    select: state => state.matches.some(match => match._notFound)
      ? null
      : state.matches.reduce<RouteTitle | null>((title, match) => match.staticData?.title ?? title, null),
  })
  const page = useSyncExternalStore(subscribePageTitle, readPageTitle)
  const disguiseTitle = useSpreadsheetDocumentTitle()

  const pageName = page.detail ?? (routeTitle ? t(routeTitle.titleKey, { ns: routeTitle.titleNs }) : undefined)
  const title = disguiseTitle ?? [page.speeds, pageName, APP_NAME].filter(Boolean).join(SEPARATOR)

  useEffect(() => {
    document.title = title
    return () => {
      document.title = APP_NAME
    }
  }, [title])
}
