/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createContext, useState, useEffect, useRef } from "react"
import type { ReactNode } from "react"

interface MobileScrollContextType {
  isFooterVisible: boolean
  setScrollContainer: (element: HTMLElement | null) => void
}

const MobileScrollContext = createContext<MobileScrollContextType | undefined>(undefined)

export function MobileScrollProvider({ children }: { children: ReactNode }) {
  const [isFooterVisible, setIsFooterVisible] = useState(true)
  const [scrollContainer, setScrollContainer] = useState<HTMLElement | null>(null)
  const lastScrollY = useRef(0)
  const ticking = useRef(false)
  const threshold = 10

  useEffect(() => {
    if (!scrollContainer) return

    const updateScrollDirection = () => {
      const scrollY = scrollContainer.scrollTop

      // Only update if we've scrolled more than the threshold
      if (Math.abs(scrollY - lastScrollY.current) < threshold) {
        ticking.current = false
        return
      }

      // Determine scroll direction
      if (scrollY > lastScrollY.current) {
        setIsFooterVisible(false) // Hide on scroll down
      } else {
        setIsFooterVisible(true) // Show on scroll up
      }

      lastScrollY.current = scrollY > 0 ? scrollY : 0
      ticking.current = false
    }

    const onScroll = () => {
      if (!ticking.current) {
        window.requestAnimationFrame(updateScrollDirection)
        ticking.current = true
      }
    }

    scrollContainer.addEventListener("scroll", onScroll)
    return () => scrollContainer.removeEventListener("scroll", onScroll)
  }, [scrollContainer])

  return (
    <MobileScrollContext.Provider value={{ isFooterVisible, setScrollContainer }}>
      {children}
    </MobileScrollContext.Provider>
  )
}
