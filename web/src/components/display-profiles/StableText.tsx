/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

interface StableTextProps {
  reserve: readonly string[]
  className?: string
  children?: ReactNode
}

export function StableText({ reserve, className, children }: StableTextProps) {
  return (
    <span className={cn("grid [&>*]:col-start-1 [&>*]:row-start-1", className)}>
      {reserve.map((text) => (
        <span key={text} aria-hidden="true" data-text={text} className="invisible before:content-[attr(data-text)]" />
      ))}
      <span>{children}</span>
    </span>
  )
}
