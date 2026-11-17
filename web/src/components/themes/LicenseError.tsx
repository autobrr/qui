/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { LICENSE_PORTAL_URL } from "@/lib/dodo-constants"
import { getLicenseErrorKey } from "@/lib/license-errors"
import { cn } from "@/lib/utils"
import type { ReactNode } from "react"
import { Trans } from "react-i18next"

// Trans fills children with the text inside the <portal> tag.
export function LicensePortalLink({ children }: { children?: ReactNode }) {
  return (
    <a
      href={LICENSE_PORTAL_URL}
      target="_blank"
      rel="noopener noreferrer"
      className="underline underline-offset-2 hover:no-underline"
    >
      {children}
    </a>
  )
}

export function LicenseError({ error, className }: { error: Error | null; className?: string }) {
  const key = getLicenseErrorKey(error)
  if (!key) return null

  return (
    <p className={cn("text-sm text-destructive", className)}>
      <Trans ns="settings" i18nKey={key} components={{ portal: <LicensePortalLink /> }} />
    </p>
  )
}
