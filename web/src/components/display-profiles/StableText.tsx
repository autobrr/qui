/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

interface StableTextProps {
  /** Every text that this place can show. The box is as tall as the longest one at the current width. */
  reserve: readonly string[]
  className?: string
  children?: ReactNode
}

/**
 * Shows children in a box that keeps one size while the text changes, so the page does not jump. Each reserved
 * text draws as invisible generated content: it takes space, but it is not in the DOM text or the accessibility tree.
 */
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
