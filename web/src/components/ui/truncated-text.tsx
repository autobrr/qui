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
  tooltipContent?: React.ReactNode
  wrap?: boolean
  openOnTap?: boolean | "only"
}

export const TruncatedText = React.forwardRef<HTMLSpanElement, TruncatedTextProps>(
  ({ children, className, tooltipSide = "top", tooltipContent, wrap = false, openOnTap = true, ...props }, ref) => {
    const [element, setElement] = React.useState<HTMLSpanElement | null>(null)
    const [hiddenText, setHiddenText] = React.useState<string | null>(null)

    React.useImperativeHandle(ref, () => element!, [element])

    React.useEffect(() => {
      if (!element) return

      const checkTruncation = () => {
        const cut = element.scrollWidth > element.clientWidth || element.scrollHeight > element.clientHeight
        setHiddenText(cut ? element.textContent : null)
      }

      checkTruncation()

      const resizeObserver = new ResizeObserver(checkTruncation)
      resizeObserver.observe(element)

      return () => resizeObserver.disconnect()
    }, [element, children])

    const text = (
      <span
        ref={setElement}
        data-slot="truncated-text"
        className={cn(!wrap && "truncate", className)}
        {...props}
      >
        {children}
      </span>
    )

    if (tooltipContent === undefined && !hiddenText) {
      return text
    }

    return (
      <Tooltip>
        <TooltipTrigger asChild openOnTap={openOnTap}>
          {text}
        </TooltipTrigger>
        <TooltipContent side={tooltipSide}>
          {tooltipContent ?? hiddenText}
        </TooltipContent>
      </Tooltip>
    )
  }
)

TruncatedText.displayName = "TruncatedText"
