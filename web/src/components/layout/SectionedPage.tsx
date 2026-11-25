/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Page } from "@/components/layout/Page"
import { ShownSectionContext } from "@/components/layout/shown-section"
import { SIDEWAYS_PHONE_QUERY, useIsMobile, useMediaQuery } from "@/hooks/useMediaQuery"
import { cn } from "@/lib/utils"
import { ChevronRight } from "lucide-react"
import { useLayoutEffect, useRef, useState, type ReactNode } from "react"

export interface SectionNavGroup<S extends string> {
  label?: string
  sections: readonly { value: S; label: string }[]
}

interface SectionedPageProps<S extends string> {
  title: string
  description?: ReactNode
  badge?: ReactNode
  notice?: ReactNode
  groups: readonly SectionNavGroup<S>[]
  section: S | undefined
  defaultSection: S
  onSectionChange: (section: S | undefined, options: { replace: boolean }) => void
  children: (section: S) => ReactNode
}

export function SectionedPage<S extends string>({ title, description, badge, notice, groups, section, defaultSection, onSectionChange, children }: SectionedPageProps<S>) {
  const rootRef = useRef<HTMLDivElement>(null)
  const sideways = useMediaQuery(SIDEWAYS_PHONE_QUERY)
  const phone = useIsMobile() || sideways
  const showMenu = phone && section === undefined
  const [menuTap, setMenuTap] = useState<{ tapped: S | undefined; seen: S | undefined }>({ tapped: undefined, seen: section })
  if (menuTap.seen !== section) setMenuTap({ tapped: section === menuTap.tapped ? section : undefined, seen: section })
  const menuScrollTop = useRef(0)

  const scroller = () => rootRef.current?.closest("main")

  useLayoutEffect(() => {
    const main = scroller()
    if (showMenu && main) main.scrollTop = menuScrollTop.current
  }, [showMenu])

  const openSection = (next: S, fromMenu: boolean) => {
    const main = scroller()
    if (main) {
      if (fromMenu) menuScrollTop.current = main.scrollTop
      main.scrollTop = 0
    }
    setMenuTap((tap) => ({ ...tap, tapped: fromMenu ? next : undefined }))
    onSectionChange(next, { replace: !fromMenu })
  }

  const backToMenu = () => {
    if (section !== undefined && menuTap.tapped === section) {
      window.history.back()
    } else {
      onSectionChange(undefined, { replace: true })
    }
  }

  if (showMenu) {
    return (
      <Page title={title} description={description} badge={badge}>
        <div ref={rootRef} className="space-y-6">
          {notice}
          <PhoneMenu label={title} groups={groups} onSelect={(next) => openSection(next, true)} />
        </div>
      </Page>
    )
  }

  const shown = section ?? defaultSection
  const sectionLabel = groups.flatMap((group) => group.sections).find((entry) => entry.value === shown)?.label ?? title
  const header = phone ? { title: sectionLabel, back: { label: title, onClick: backToMenu } } : { title, description, badge }

  return (
    <Page {...header}>
      {notice}
      <div ref={rootRef} className={cn(!phone && "flex items-start gap-6")}>
        {!phone && (
          <nav className="sticky top-4 max-h-[calc(100dvh-7rem)] w-56 shrink-0 space-y-5 overflow-y-auto lg:top-6" aria-label={title}>
            {groups.map((group) => (
              <div key={group.sections[0].value} className="space-y-1">
                {group.label && <p className="px-3 pb-1 text-sm font-semibold text-foreground">{group.label}</p>}
                {group.sections.map((entry) => (
                  <button
                    key={entry.value}
                    type="button"
                    onClick={() => openSection(entry.value, false)}
                    aria-current={shown === entry.value ? "page" : undefined}
                    className={cn(
                      "w-full rounded-md px-3 py-1.5 text-left text-sm font-medium transition-colors",
                      shown === entry.value ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
                    )}
                  >
                    {entry.label}
                  </button>
                ))}
              </div>
            ))}
          </nav>
        )}

        <div className={cn("space-y-6", !phone && "min-w-0 max-w-5xl flex-1")}>
          <ShownSectionContext.Provider value={phone ? sectionLabel : null}>{children(shown)}</ShownSectionContext.Provider>
        </div>
      </div>
    </Page>
  )
}

function PhoneMenu<S extends string>({ label, groups, onSelect }: { label: string; groups: readonly SectionNavGroup<S>[]; onSelect: (section: S) => void }) {
  return (
    <nav aria-label={label} className="space-y-6">
      {groups.map((group) => (
        <div key={group.sections[0].value} className="space-y-2">
          {group.label && <h2 className="px-1 text-sm font-medium text-muted-foreground">{group.label}</h2>}
          <ul className="divide-y overflow-hidden rounded-xl border bg-card">
            {group.sections.map((entry) => (
              <li key={entry.value}>
                <button
                  type="button"
                  onClick={() => onSelect(entry.value)}
                  className="flex min-h-12 w-full items-center gap-3 px-4 text-left text-[15px] font-medium outline-none transition-colors active:bg-accent hover:bg-accent/50 focus-visible:bg-accent/50"
                >
                  <span className="min-w-0 flex-1">{entry.label}</span>
                  <ChevronRight aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
                </button>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </nav>
  )
}
