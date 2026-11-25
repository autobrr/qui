/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, type RefObject } from "react"
import { type Virtualizer } from "@tanstack/react-virtual"

const LOAD_MORE_THRESHOLD = 50

interface UseKeyboardNavigationProps {
  parentRef: RefObject<HTMLDivElement | null>
  virtualizer: Virtualizer<HTMLDivElement, Element>
  safeLoadedRows: number
  hasLoadedAll: boolean
  isLoadingMore: boolean
  loadMore: () => void
  estimatedRowHeight?: number
}

export function useKeyboardNavigation({
  parentRef,
  virtualizer,
  safeLoadedRows,
  hasLoadedAll,
  isLoadingMore,
  loadMore,
  estimatedRowHeight = 40,
}: UseKeyboardNavigationProps) {

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement
      if (
        target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.contentEditable === "true" ||
        target.closest("[role=\"dialog\"]") ||
        target.closest("[role=\"combobox\"]")
      ) {
        return
      }

      const { key } = event
      const container = parentRef.current

      if (!container) return

      const navigationKeys = ["PageUp", "PageDown", "Home", "End"]
      if (!navigationKeys.includes(key)) return

      event.preventDefault()
      event.stopPropagation()

      const prefersReducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches
      const scrollBehavior = prefersReducedMotion ? "auto" : "smooth"

      const viewportHeight = container.clientHeight
      const rowsPerPage = Math.floor(viewportHeight / estimatedRowHeight)
      const currentScrollTop = container.scrollTop
      const currentRowIndex = Math.floor(currentScrollTop / estimatedRowHeight)

      switch (key) {
        case "PageUp": {
          const targetIndex = Math.max(0, currentRowIndex - rowsPerPage)
          const targetOffset = targetIndex * estimatedRowHeight

          container.scrollTo({
            top: targetOffset,
            behavior: scrollBehavior,
          })

          if (targetIndex >= safeLoadedRows - LOAD_MORE_THRESHOLD && !hasLoadedAll && !isLoadingMore) {
            loadMore()
          }
          break
        }

        case "PageDown": {
          const targetIndex = Math.min(
            safeLoadedRows - 1,
            currentRowIndex + rowsPerPage
          )
          const targetOffset = targetIndex * estimatedRowHeight

          container.scrollTo({
            top: targetOffset,
            behavior: scrollBehavior,
          })

          if (targetIndex >= safeLoadedRows - LOAD_MORE_THRESHOLD && !hasLoadedAll && !isLoadingMore) {
            loadMore()
          }
          break
        }

        case "Home": {
          container.scrollTo({
            top: 0,
            behavior: scrollBehavior,
          })
          break
        }

        case "End": {
          if (hasLoadedAll) {
            const lastItemOffset = (safeLoadedRows - 1) * estimatedRowHeight
            const targetOffset = Math.max(0, lastItemOffset - viewportHeight + estimatedRowHeight)

            container.scrollTo({
              top: targetOffset,
              behavior: scrollBehavior,
            })
          } else {
            const totalSize = virtualizer.getTotalSize()
            const targetOffset = Math.max(0, totalSize - viewportHeight)

            container.scrollTo({
              top: targetOffset,
              behavior: scrollBehavior,
            })

            if (!isLoadingMore) {
              loadMore()
            }
          }
          break
        }
      }
    }

    window.addEventListener("keydown", handleKeyDown)

    return () => {
      window.removeEventListener("keydown", handleKeyDown)
    }
  }, [virtualizer, safeLoadedRows, hasLoadedAll, isLoadingMore, loadMore, estimatedRowHeight, parentRef])
}
