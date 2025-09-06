/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, type RefObject } from "react"
import { type Virtualizer } from "@tanstack/react-virtual"

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
  // Smooth scroll using offset approach to avoid TanStack Virtual retry issues
  const smoothScrollToIndex = useCallback((index: number, align: "start" | "center" | "end" = "start") => {
    // Validate index bounds
    if (index < 0 || index >= safeLoadedRows) {
      console.warn(`ScrollToIndex: Index ${index} out of bounds (0-${safeLoadedRows - 1})`)
      return false
    }

    if (!parentRef.current) return false

    // Calculate target offset based on index and alignment
    let targetOffset = index * estimatedRowHeight

    if (align === "center") {
      const viewportHeight = parentRef.current.clientHeight
      targetOffset -= viewportHeight / 2
    } else if (align === "end") {
      const viewportHeight = parentRef.current.clientHeight
      targetOffset -= viewportHeight - estimatedRowHeight
    }

    // Clamp offset to valid range
    const maxOffset = virtualizer.getTotalSize() - parentRef.current.clientHeight
    targetOffset = Math.max(0, Math.min(targetOffset, maxOffset))

    // Use smooth scrolling via the container element
    parentRef.current.scrollTo({
      top: targetOffset,
      behavior: "smooth",
    })

    return true
  }, [virtualizer, safeLoadedRows, estimatedRowHeight, parentRef])

  // Keyboard navigation handler
  const handleKeyboardNavigation = useCallback((event: KeyboardEvent) => {
    const { key } = event

    if (!parentRef.current) return

    const viewportHeight = parentRef.current.clientHeight
    const rowsPerPage = Math.floor(viewportHeight / estimatedRowHeight)
    const currentScrollTop = parentRef.current.scrollTop
    const currentRowIndex = Math.floor(currentScrollTop / estimatedRowHeight)

    switch (key) {
      case "PageUp":
      case "Page Up":
      case "Prior": {
        event.preventDefault()
        const pageUpIndex = Math.max(0, currentRowIndex - rowsPerPage)
        smoothScrollToIndex(pageUpIndex, "start")
        // Trigger loading if needed
        if (pageUpIndex >= safeLoadedRows - 50 && !hasLoadedAll && !isLoadingMore) {
          loadMore()
        }
        break
      }

      case "PageDown":
      case "Page Down":
      case "Next": {
        event.preventDefault()
        const pageDownIndex = Math.min(
          safeLoadedRows - 1,
          currentRowIndex + rowsPerPage
        )
        smoothScrollToIndex(pageDownIndex, "start")
        // Trigger loading if needed
        if (pageDownIndex >= safeLoadedRows - 50 && !hasLoadedAll && !isLoadingMore) {
          loadMore()
        }
        break
      }

      case "Home": {
        event.preventDefault()
        smoothScrollToIndex(0, "start")
        break
      }

      case "End": {
        event.preventDefault()
        if (hasLoadedAll) {
          smoothScrollToIndex(safeLoadedRows - 1, "end")
        } else {
          // For End key with progressive loading, use offset-based approach
          // to avoid scrollToIndex issues with unloaded content
          const totalSize = virtualizer.getTotalSize()
          const viewportHeight = parentRef.current.clientHeight
          const targetOffset = Math.max(0, totalSize - viewportHeight)
          parentRef.current.scrollTo({
            top: targetOffset,
            behavior: "smooth",
          })

          // Trigger loading if needed
          if (!isLoadingMore) {
            loadMore()
          }
        }
        break
      }
    }
  }, [virtualizer, safeLoadedRows, hasLoadedAll, isLoadingMore, loadMore, estimatedRowHeight, parentRef, smoothScrollToIndex])

  // Set up keyboard event listeners
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      // Only handle navigation keys when table container is focused or contains focused element
      const target = event.target as HTMLElement
      const isTableFocused = parentRef.current?.contains(target) || target === parentRef.current

      if (!isTableFocused) {
        return
      }

      // Handle different possible key names for Page Up/Down
      const navigationKeys = [
        "PageUp", "Page Up",
        "PageDown", "Page Down",
        "Home",
        "End",
        // Some browsers might use these legacy names
        "Prior", "Next",
      ]

      if (navigationKeys.includes(event.key)) {
        event.preventDefault() // Prevent browser default scroll behavior
        event.stopPropagation() // Stop event from bubbling
        handleKeyboardNavigation(event)
      }
    }

    // Use window-level listener to catch all events
    window.addEventListener("keydown", handleKeyDown, true) // Use capture phase

    const container = parentRef.current
    if (container) {
      container.setAttribute("tabIndex", "0")
      container.style.outline = "none" // Remove focus outline
      // Focus the container initially to make sure it can receive events
      container.focus()
    }

    return () => {
      window.removeEventListener("keydown", handleKeyDown, true)
    }
  }, [handleKeyboardNavigation, parentRef])
}