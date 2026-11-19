/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { HardDrive } from "lucide-react"

import { instanceMarkColorProps, useInstanceMark, type InstanceMark } from "@/lib/instance-marks"
import { cn } from "@/lib/utils"

import { findInstanceMarkIcon } from "./instance-mark-icons"

interface MarkGlyphProps {
  mark: InstanceMark
  /** Shows the letters of a mark without an icon. Without it, such a mark shows the drive icon. */
  letters?: string
  /** The instance has a problem, for example it is disconnected: the mark takes the destructive color. */
  problem?: boolean
  /** False keeps the text color, for example on the highlighted row of a list. A problem still shows. */
  colored?: boolean
  className?: string
}

/** A mark as it would show: its icon, or its letters, in its color. Takes the size of a 16px icon by default. */
export function MarkGlyph({ mark, letters, problem = false, colored = true, className }: MarkGlyphProps) {
  const Icon = findInstanceMarkIcon(mark.icon)
  const color = problem ? { className: "text-destructive" } : colored ? instanceMarkColorProps(mark.color) : {}

  if (!Icon && letters) {
    return (
      <span aria-hidden="true" className={cn("flex size-4 shrink-0 items-center justify-center text-[11px] font-semibold leading-none whitespace-nowrap", color.className, className)} style={color.style}>
        {letters}
      </span>
    )
  }
  const Glyph = Icon ?? HardDrive
  return <Glyph aria-hidden="true" className={cn("size-4 shrink-0", color.className, className)} style={color.style} />
}

interface InstanceMarkIconProps extends Omit<MarkGlyphProps, "mark" | "letters"> {
  /** The id of the instance, or ALL_INSTANCES_ID for Unified. */
  instanceId: number
  /**
   * Shows the mark as the icon-only sidebar does: the letters of the mark, or these initials while it has none. Without
   * it, a mark without an icon shows the drive icon, as everywhere else.
   */
  autoLetters?: string
}

/** The stored mark of an instance. By default it shows as an icon, since only the icon-only sidebar shows letters. */
export function InstanceMarkIcon({ instanceId, autoLetters, ...props }: InstanceMarkIconProps) {
  const [mark] = useInstanceMark(instanceId)
  return <MarkGlyph mark={mark} letters={autoLetters === undefined ? undefined : mark.letters || autoLetters} {...props} />
}
