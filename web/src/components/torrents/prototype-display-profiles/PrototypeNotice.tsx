/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway). The real FeatureNotice renders nothing in the demo, so
// this copies its bubble markup and shows it while ?notice=1 is set.

import * as PopperPrimitive from "@radix-ui/react-popper"
import { Laptop, Monitor } from "lucide-react"
import type { ReactNode } from "react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { actions, useNoticeVisible } from "./store"

function Screen({ wide, columns, label }: { wide?: boolean; columns: number; label: string }) {
  const Icon = wide ? Monitor : Laptop
  return (
    <div className="flex flex-col items-center gap-1">
      <div className={cn("flex flex-col gap-[3px] rounded-sm bg-background p-1 ring-1 ring-foreground/15", wide ? "w-28" : "w-16")}>
        {[0, 1, 2].map((row) => (
          <div key={row} className="flex gap-[2px]">
            {Array.from({ length: columns }, (_, i) => (
              <span key={i} className={cn("h-1 rounded-full bg-muted-foreground/50", i === 0 ? "flex-[3]" : "flex-1")} />
            ))}
          </div>
        ))}
      </div>
      <span className="flex items-center gap-1 text-[10px] text-muted-foreground"><Icon className="h-2.5 w-2.5" />{label}</span>
    </div>
  )
}

function NoticePicture() {
  return (
    <div aria-hidden="true" className="flex items-end justify-center gap-4 rounded-md bg-foreground/[0.07] p-2">
      <Screen wide columns={7} label="Workstation" />
      <Screen columns={3} label="Laptop" />
    </div>
  )
}

interface PrototypeNoticeProps {
  /** False renders the control alone. */
  enabled: boolean
  side?: "top" | "bottom"
  children: ReactNode
}

export function PrototypeNotice({ enabled, side = "bottom", children }: PrototypeNoticeProps) {
  const open = useNoticeVisible()
  if (!enabled) return children
  return (
    <PopperPrimitive.Root>
      <PopperPrimitive.Anchor
        asChild
        className={open ? "motion-safe:animate-feature-notice-pulse motion-reduce:ring-2 motion-reduce:ring-primary/30" : undefined}
      >
        {children}
      </PopperPrimitive.Anchor>
      {open && (
        <PopperPrimitive.Content
          side={side}
          sideOffset={5}
          collisionPadding={8}
          role="region"
          aria-label="New: Display profiles"
          data-prototype-notice=""
          className="z-50 w-80 rounded-xl border border-foreground/15 bg-popover p-3 text-popover-foreground shadow-xl outline-none motion-safe:animate-in motion-safe:fade-in-0 motion-safe:zoom-in-95"
        >
          <NoticePicture />
          <p className="pt-3 text-sm font-semibold leading-5">Display profiles</p>
          <p className="mt-1 text-sm leading-5 text-muted-foreground text-balance">
            Keep a different column setup on each browser. Each browser picks its own profile.
          </p>
          <div className="mt-3 flex justify-end gap-1">
            <Button size="sm" variant="ghost" className="text-muted-foreground" onClick={actions.dismissNotice}>Got it</Button>
            <Button size="sm" onClick={actions.noticeAction}>Show profiles</Button>
          </div>
          <PopperPrimitive.Arrow asChild>
            <svg width="20" height="10" viewBox="0 0 20 10" className="block overflow-visible">
              <path d="M0 -2 L0 0 L10 9 L20 0 L20 -2 Z" className="fill-popover" />
              <path d="M0 -0.5 L10 8.5 L20 -0.5" fill="none" className="stroke-foreground/15" />
            </svg>
          </PopperPrimitive.Arrow>
        </PopperPrimitive.Content>
      )}
    </PopperPrimitive.Root>
  )
}
