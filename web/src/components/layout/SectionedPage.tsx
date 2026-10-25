/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from "@/components/ui/select"
import { cn } from "@/lib/utils"
import { useRef, type ReactNode } from "react"

/** A group without a label shows its sections without a heading. Labels arrive translated. */
export interface SectionNavGroup<S extends string> {
  label?: string
  sections: readonly { value: S; label: string }[]
}

interface SectionedPageProps<S extends string> {
  /** Accessible name of the section nav, usually the page title. */
  navLabel: string
  groups: readonly SectionNavGroup<S>[]
  activeSection: S
  onSectionChange: (section: S) => void
  children: ReactNode
}

export function SectionedPage<S extends string>({ navLabel, groups, activeSection, onSectionChange, children }: SectionedPageProps<S>) {
  const rootRef = useRef<HTMLDivElement>(null)

  // The router resets only the window scroll, and the page scrolls inside <main>.
  const changeSection = (section: S) => {
    const main = rootRef.current?.closest("main")
    if (main) main.scrollTop = 0
    onSectionChange(section)
  }

  return (
    <div ref={rootRef} className="flex flex-col gap-6 md:flex-row md:items-start">
      <div className="md:hidden">
        <Select value={activeSection} onValueChange={(value) => changeSection(value as S)}>
          <SelectTrigger className="w-full min-h-11" aria-label={navLabel}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {groups.map(group => (
              <SelectGroup key={group.sections[0].value}>
                {group.label && <SelectLabel>{group.label}</SelectLabel>}
                {group.sections.map(section => (
                  <SelectItem key={section.value} value={section.value}>{section.label}</SelectItem>
                ))}
              </SelectGroup>
            ))}
          </SelectContent>
        </Select>
      </div>

      <nav className="hidden w-56 shrink-0 space-y-5 md:sticky md:top-4 md:block md:max-h-[calc(100dvh-7rem)] md:overflow-y-auto lg:top-6" aria-label={navLabel}>
        {groups.map(group => (
          <div key={group.sections[0].value} className="space-y-1">
            {group.label && <p className="px-3 pb-1 text-sm font-semibold text-foreground">{group.label}</p>}
            {group.sections.map(section => (
              <button
                key={section.value}
                type="button"
                onClick={() => changeSection(section.value)}
                aria-current={activeSection === section.value ? "page" : undefined}
                className={cn(
                  "w-full rounded-md px-3 py-1.5 text-left text-sm font-medium transition-colors",
                  activeSection === section.value ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
                )}
              >
                {section.label}
              </button>
            ))}
          </div>
        ))}
      </nav>

      <div className="min-w-0 max-w-5xl flex-1 space-y-6">
        {children}
      </div>
    </div>
  )
}
