/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
import { ExternalLink } from "lucide-react"

const URL_REGEX = /(https?:\/\/[^\s<>"{}|\\^`[\]]+)/gi

export function renderTextWithLinks(text: string): React.ReactNode {
  if (!text) return text

  const parts = text.split(URL_REGEX)

  return parts.map((part, index) => {
    if (URL_REGEX.test(part)) {
      URL_REGEX.lastIndex = 0

      const trailingPunctuationMatch = part.match(/^(.*?)([)\]}.,;!?]*?)$/)
      const cleanUrl = trailingPunctuationMatch ? trailingPunctuationMatch[1] : part
      const trailingPunctuation = trailingPunctuationMatch ? trailingPunctuationMatch[2] : ""

      return (
        <React.Fragment key={index}>
          <a
            href={cleanUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary hover:underline break-all"
          >
            <span className="inline-flex items-center gap-1">
              <span>{cleanUrl}</span>
              <ExternalLink className="h-3.5 w-3.5" aria-hidden="true" />
            </span>
          </a>
          {trailingPunctuation && <span>{trailingPunctuation}</span>}
        </React.Fragment>
      )
    }

    return <span key={index}>{part}</span>
  })
}

export function containsLinks(text: string): boolean {
  if (!text) return false
  URL_REGEX.lastIndex = 0
  return URL_REGEX.test(text)
}
