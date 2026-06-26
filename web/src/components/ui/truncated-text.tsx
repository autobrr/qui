/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cn } from "@/lib/utils"
import * as React from "react"

import { Tooltip, TooltipContent, TooltipTrigger } from "./tooltip"

interface TruncatedTextProps extends React.HTMLAttributes<HTMLSpanElement> {
  children: React.ReactNode
  tooltipSide?: "top" | "right" | "bottom" | "left"
  /** Custom tooltip content. If provided, always shows on hover regardless of truncation. */
  tooltipContent?: React.ReactNode
}

/**
 * A text component that shows a tooltip only when the text is truncated.
 * Uses ResizeObserver to detect truncation dynamically.
 *
 * If `tooltipContent` is provided, it always shows on hover.
 * Otherwise, the tooltip only appears when text is truncated.
 */
export const TruncatedText = React.forwardRef<HTMLSpanElement, TruncatedTextProps>(
  ({ children, className, tooltipSide = "top", tooltipContent, ...props }, ref) => {
    const innerRef = React.useRef<HTMLSpanElement>(null)
    const [isTruncated, setIsTruncated] = React.useState(false)

    // Merge refs
    React.useImperativeHandle(ref, () => innerRef.current!)

    React.useEffect(() => {
      const element = innerRef.current
      if (!element) return

      const checkTruncation = () => {
        setIsTruncated(element.scrollWidth > element.clientWidth)
      }

      // Initial check
      checkTruncation()

      // Watch for size changes
      const resizeObserver = new ResizeObserver(checkTruncation)
      resizeObserver.observe(element)

      return () => resizeObserver.disconnect()
    }, [children])

    const textContent = typeof children === "string" ? children : undefined
    // Show tooltip if: custom content provided, OR text is truncated and we have text content
    const showTooltip = tooltipContent !== undefined || (isTruncated && textContent)
    const finalTooltipContent = tooltipContent ?? textContent

    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span
            ref={innerRef}
            className={cn("truncate", className)}
            {...props}
          >
            {children}
          </span>
        </TooltipTrigger>
        {showTooltip && (
          <TooltipContent side={tooltipSide}>
            {finalTooltipContent}
          </TooltipContent>
        )}
      </Tooltip>
    )
  }
)

TruncatedText.displayName = "TruncatedText"
