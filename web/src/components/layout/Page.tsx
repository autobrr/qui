/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cn } from "@/lib/utils"
import { ChevronLeft } from "lucide-react"
import type { ReactNode } from "react"

interface PageProps {
  title: ReactNode
  description?: ReactNode
  badge?: ReactNode
  controls?: ReactNode
  fill?: boolean
  back?: { label: string; onClick: () => void }
  children: ReactNode
}

export function Page({ title, description, badge, controls, fill = false, back, children }: PageProps) {
  return (
    <div className={cn("p-4 lg:p-6", fill && "flex h-full flex-col overflow-hidden")}>
      <div className="mb-6 flex shrink-0 flex-wrap items-start gap-x-4 gap-y-3">
        <div className="min-w-0 flex-auto space-y-1">
          {back && (
            <button
              type="button"
              onClick={back.onClick}
              className="-ml-2 flex min-h-10 items-center gap-0.5 rounded-md pr-2 pl-1 text-sm font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              <ChevronLeft className="size-5" />
              {back.label}
            </button>
          )}
          <div className="flex min-h-9 items-center gap-x-4">
            <h1 className="mr-auto text-2xl font-semibold tracking-tight">{title}</h1>
            {badge}
          </div>
          {description && <p className="text-sm text-muted-foreground">{description}</p>}
        </div>
        {controls && <div className="flex min-w-0 max-w-full flex-wrap items-center gap-1.5 max-sm:w-full max-sm:flex-nowrap sm:gap-4">{controls}</div>}
      </div>
      <div className={fill ? "flex min-h-0 flex-1 flex-col" : "space-y-6"}>{children}</div>
    </div>
  )
}
