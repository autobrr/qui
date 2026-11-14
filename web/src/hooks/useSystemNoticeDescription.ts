/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTranslation } from "react-i18next"

import { systemNoticeRoutes, type SystemNoticeRoute } from "@/lib/system-notice"

export type SystemNoticeDescription = {
  short: string
  title: string
  effect: string
  fix: string
  menuFix: string
  action?: SystemNoticeRoute & { label: string }
}

type TextField = Exclude<keyof SystemNoticeDescription, "action">

// useSystemNoticeDescription returns the texts of one kind of System notice.
// A kind that this frontend does not know gets the generic texts.
export function useSystemNoticeDescription(kind: string): SystemNoticeDescription {
  const { t } = useTranslation("common")
  const text = (field: TextField) => t([`systemNotice.kinds.${kind}.${field}`, `systemNotice.unknown.${field}`])
  const route = systemNoticeRoutes[kind]

  return {
    short: text("short"),
    title: text("title"),
    effect: text("effect"),
    fix: text("fix"),
    menuFix: text("menuFix"),
    action: route && { ...route, label: t([`systemNotice.kinds.${kind}.action`, "systemNotice.actionFallback"]) },
  }
}
