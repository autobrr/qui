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
  letters?: string
  problem?: boolean
  colored?: boolean
  className?: string
}

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
  instanceId: number
  autoLetters?: string
}

export function InstanceMarkIcon({ instanceId, autoLetters, ...props }: InstanceMarkIconProps) {
  const [mark] = useInstanceMark(instanceId)
  return <MarkGlyph mark={mark} letters={autoLetters === undefined ? undefined : mark.letters || autoLetters} {...props} />
}
