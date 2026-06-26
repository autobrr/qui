/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
import { ExternalLink } from "lucide-react"

// Regular expression to match URLs
const URL_REGEX = /(https?:\/\/[^\s<>"{}|\\^`[\]]+)/gi

/**
 * Converts plain text with URLs into React elements with clickable links
 * @param text The text that may contain URLs
 * @returns React elements with clickable links
 */
export function renderTextWithLinks(text: string): React.ReactNode {
  if (!text) return text

  const parts = text.split(URL_REGEX)

  return parts.map((part, index) => {
    // Check if this part is a URL
    if (URL_REGEX.test(part)) {
      URL_REGEX.lastIndex = 0 // Reset regex state

      // Remove trailing punctuation from URLs
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

    // Regular text
    return <span key={index}>{part}</span>
  })
}

/**
 * Checks if the given text contains any URLs
 * @param text The text to check
 * @returns true if the text contains URLs, false otherwise
 */
export function containsLinks(text: string): boolean {
  if (!text) return false
  URL_REGEX.lastIndex = 0 // Reset regex state
  return URL_REGEX.test(text)
}
