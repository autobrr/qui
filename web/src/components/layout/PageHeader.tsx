/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ReactNode } from "react"

interface PageHeaderProps {
  title: ReactNode
  description?: ReactNode
  /**
   * Page actions, for example the instance picker. Each action shares the title row while it fits and
   * wraps below the title on its own when it does not, so pass several actions as a fragment.
   */
  actions?: ReactNode
}

export function PageHeader({ title, description, actions }: PageHeaderProps) {
  return (
    <div className="space-y-1">
      <div className="flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
        <h1 className="mr-auto text-2xl font-semibold tracking-tight">{title}</h1>
        {actions}
      </div>
      {description && <p className="text-sm text-muted-foreground">{description}</p>}
    </div>
  )
}
