/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useState, useRef } from "react"
import { ArrowUp } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { useMobileScroll } from "@/contexts/MobileScrollContext"
import { useTorrentSelectionContext } from "@/contexts/TorrentSelectionContext"
import { useIsMobile } from "@/hooks/useMediaQuery"
import { hasSelection } from "@/lib/torrent-selection"
import { cn } from "@/lib/utils"

const threshold = 300

interface ScrollToTopButtonProps {
  scrollContainerRef: React.RefObject<HTMLElement | null>
  /** Height in rem of the card list's own fixed bars at the bottom, above or in place of the bottom navigation. */
  extraBottomBarsRem?: number
}

// Shows past the scroll threshold at every width, and never covers the bottom
// navigation or the caller's bars.
export function ScrollToTopButton({
  scrollContainerRef,
  extraBottomBarsRem = 0,
}: ScrollToTopButtonProps) {
  const { t } = useTranslation("common")
  const isMobile = useIsMobile()
  const { isFooterVisible } = useMobileScroll()
  const { selection } = useTorrentSelectionContext()
  // MobileFooterNav renders nothing in selection mode
  const barsRem = (isFooterVisible && !hasSelection(selection) ? 4 : 0) + extraBottomBarsRem
  const gapRem = barsRem > 0 ? 1 : 1.5
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
  }, [scrollContainerRef])

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
        "z-30 shadow-lg bg-background/80 backdrop-blur-sm transition-all duration-300",
        isMobile ? "fixed right-8" : "absolute bottom-4 right-6",
        isVisible ? "opacity-100 translate-y-0" : "opacity-0 translate-y-2 pointer-events-none"
      )}
      style={isMobile ? { bottom: `calc(${barsRem + gapRem}rem + env(safe-area-inset-bottom))` } : undefined}
      aria-label={t("actions.scrollToTop")}
      aria-hidden={!isVisible}
      tabIndex={isVisible ? undefined : -1}
    >
      <ArrowUp className="h-4 w-4" />
    </Button>
  )
}
