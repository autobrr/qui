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
  /** A short status beside the title, for example a badge. It is not a control. */
  badge?: ReactNode
  /**
   * The controls of the page, for example the instance picker or a period picker. Pass them as a fragment, each at
   * the default height (h-9). They sit beside the title when they fit there. When they do not, and always on a phone,
   * they take one row below the description. They never move the title or the description.
   */
  controls?: ReactNode
  /** The content fills the height below the header and scrolls inside itself. Without it, the page scrolls. */
  fill?: boolean
  /** A link back to the parent view, above the title, for example from a section to the menu of its page. */
  back?: { label: string; onClick: () => void }
  /** The content. Without fill, the page puts its direct children 24 px apart. */
  children: ReactNode
}

/**
 * The shell of an authenticated page: the padding, the title, the description, the controls, and the gap to the
 * content. Each page except the torrent list and the instance pages renders one Page, also while it loads, so that
 * the title and the description are at the same place on each page and at each moment.
 */
export function Page({ title, description, badge, controls, fill = false, back, children }: PageProps) {
  return (
    // Below lg, AppLayout pads main for the bottom nav, so the page adds no bottom padding of its own.
    // With fill, the page is as high as the content box of main, which AppLayout sizes to the screen.
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
          {/* The title row is as high as a control, so that a control beside it does not move the title. */}
          <div className="flex min-h-9 items-center gap-x-4">
            <h1 className="mr-auto text-2xl font-semibold tracking-tight">{title}</h1>
            {badge}
          </div>
          {description && <p className="text-sm text-muted-foreground">{description}</p>}
        </div>
        {/* On a phone, the row is as wide as the page, and its controls shrink to stay on one line. */}
        {controls && <div className="flex min-w-0 max-w-full flex-wrap items-center gap-1.5 max-sm:w-full max-sm:flex-nowrap sm:gap-4">{controls}</div>}
      </div>
      <div className={fill ? "flex min-h-0 flex-1 flex-col" : "space-y-6"}>{children}</div>
    </div>
  )
}
