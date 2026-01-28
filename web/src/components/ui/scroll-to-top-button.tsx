/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useState, useRef } from "react"
import { ArrowUp } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

interface ScrollToTopButtonProps {
  scrollContainerRef: React.RefObject<HTMLElement | null>
  threshold?: number
  className?: string
}

export function ScrollToTopButton({
  scrollContainerRef,
  threshold = 300,
  className,
}: ScrollToTopButtonProps) {
  const [isVisible, setIsVisible] = useState(false)
  const animationFrameRef = useRef<number | undefined>(undefined)

  useEffect(() => {
    const container = scrollContainerRef.current
    if (!container) return

    const handleScroll = () => {
      if (animationFrameRef.current) {
        cancelAnimationFrame(animationFrameRef.current)
      }

      animationFrameRef.current = requestAnimationFrame(() => {
        setIsVisible(container.scrollTop > threshold)
      })
    }

    container.addEventListener("scroll", handleScroll, { passive: true })

    return () => {
      container.removeEventListener("scroll", handleScroll)
      if (animationFrameRef.current) {
        cancelAnimationFrame(animationFrameRef.current)
      }
    }
  }, [scrollContainerRef, threshold])

  const scrollToTop = () => {
    scrollContainerRef.current?.scrollTo({
      top: 0,
      behavior: "smooth",
    })
  }

  return (
    <Button
      onClick={scrollToTop}
      variant="outline"
      size="icon"
      className={cn(
        "fixed z-10 shadow-lg bg-background/80 backdrop-blur-sm transition-all duration-200",
        isVisible ? "opacity-100 translate-y-0" : "opacity-0 translate-y-2 pointer-events-none",
        className
      )}
      aria-label="Scroll to top"
    >
      <ArrowUp className="h-4 w-4" />
    </Button>
  )
}