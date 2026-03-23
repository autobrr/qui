/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cn } from "@/lib/utils"
import { useEffect, useRef, useState, type FormEventHandler, type ReactNode } from "react"

interface PreferencesFormShellProps {
  children: ReactNode
  footer: ReactNode
  onSubmit: FormEventHandler<HTMLFormElement>
  className?: string
  contentClassName?: string
  footerClassName?: string
}

export function PreferencesFormShell({
  children,
  footer,
  onSubmit,
  className,
  contentClassName,
  footerClassName,
}: PreferencesFormShellProps) {
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const contentRef = useRef<HTMLDivElement | null>(null)
  const [showTopFade, setShowTopFade] = useState(false)
  const [showBottomFade, setShowBottomFade] = useState(false)

  useEffect(() => {
    const scrollElement = scrollRef.current
    const contentElement = contentRef.current

    if (!scrollElement) {
      return
    }

    const updateFades = () => {
      const nextShowTopFade = scrollElement.scrollTop > 4
      const nextShowBottomFade = scrollElement.scrollTop + scrollElement.clientHeight < scrollElement.scrollHeight - 4

      setShowTopFade(nextShowTopFade)
      setShowBottomFade(nextShowBottomFade)
    }

    updateFades()

    const resizeObserver = typeof ResizeObserver === "undefined"? null: new ResizeObserver(() => {
      updateFades()
    })

    scrollElement.addEventListener("scroll", updateFades, { passive: true })
    window.addEventListener("resize", updateFades)
    resizeObserver?.observe(scrollElement)
    if (contentElement) {
      resizeObserver?.observe(contentElement)
    }

    return () => {
      scrollElement.removeEventListener("scroll", updateFades)
      window.removeEventListener("resize", updateFades)
      resizeObserver?.disconnect()
    }
  }, [])

  return (
    <form onSubmit={onSubmit} className={cn("flex min-h-0 flex-1 flex-col", className)}>
      <div className="relative flex min-h-0 flex-1 flex-col">
        <div
          className={cn(
            "pointer-events-none absolute inset-x-0 top-0 z-10 h-8 bg-linear-to-b from-background via-background/50 to-transparent transition-opacity duration-150",
            showTopFade ? "opacity-100" : "opacity-0"
          )}
        />
        <div
          className={cn(
            "pointer-events-none absolute inset-x-0 bottom-0 z-10 h-8 bg-linear-to-t from-background via-background/50 to-transparent transition-opacity duration-150",
            showBottomFade ? "opacity-100" : "opacity-0"
          )}
        />
        <div ref={scrollRef} className={cn("min-h-0 flex-1 overflow-y-auto md:pr-4 pb-8", contentClassName)}>
          <div ref={contentRef}>
            {children}
          </div>
        </div>
      </div>
      <div
        className={cn(
          "shrink-0 border-t pt-5 pb-[calc(env(safe-area-inset-bottom)+0.5rem)] backdrop-blur supports-backdrop-filter:bg-background/80",
          footerClassName
        )}
      >
        <div className="flex justify-end">
          {footer}
        </div>
      </div>
    </form>
  )
}
