/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useIsPhone } from "@/hooks/useMediaQuery"
import type { ReactNode } from "react"

interface PageHeaderProps {
  title: ReactNode
  description?: ReactNode
  /**
   * Page actions, for example the instance picker. Each action shares the title row while it fits and
   * wraps below the title on its own when it does not, so pass several actions as a fragment.
   */
  actions?: ReactNode
  /**
   * Page controls that stay together, for example a period picker and its filters. They share the title row on a wide
   * screen and take one row below the description on a phone, so the title and the description keep their place.
   * Give each control the default height (h-9).
   */
  controls?: ReactNode
}

export function PageHeader({ title, description, actions, controls }: PageHeaderProps) {
  const phone = useIsPhone()
  return (
    <div data-page-header className="space-y-1">
      <div className="flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
        <h1 className="mr-auto text-2xl font-semibold tracking-tight">{title}</h1>
        {actions}
        {!phone && controls}
      </div>
      {description && <p className="text-sm text-muted-foreground">{description}</p>}
      {phone && controls && <div className="flex min-w-0 items-center gap-1.5 pt-2">{controls}</div>}
    </div>
  )
}
