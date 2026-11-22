/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Fragment } from "react"

import { TruncatedText } from "@/components/ui/truncated-text"
import { cn } from "@/lib/utils"

/** A torrent name that wraps after a dot, a dash, or an underscore, and inside a word only when the word does not fit. */
export function TorrentName({ name, className, openOnTap }: { name: string; className?: string; openOnTap?: boolean }) {
  const parts = name.split(/([._-])/)
  // A file extension stays on the line of the word before it, so that ".iso" never ends a name alone.
  const extensionDot = parts.length > 2 && parts.at(-2) === "." && parts.at(-1)!.length <= 4 ? parts.length - 2 : -1
  return (
    <TruncatedText wrap openOnTap={openOnTap} className={cn("whitespace-normal [overflow-wrap:anywhere]", className)}>
      {parts.map((part, index) => <Fragment key={index}>{part}{index % 2 === 1 && index !== extensionDot && <wbr />}</Fragment>)}
    </TruncatedText>
  )
}
