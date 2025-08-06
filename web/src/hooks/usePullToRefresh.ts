import { useEffect, useRef, useState } from 'react'

interface UsePullToRefreshOptions {
  onRefresh: () => Promise<void> | void
  threshold?: number
  resistance?: number
  enabled?: boolean
}

interface PullToRefreshState {
  isPulling: boolean
  pullDistance: number
  isRefreshing: boolean
  canRefresh: boolean
}

export function usePullToRefresh({
  onRefresh,
  threshold = 80,
  resistance = 2.5,
  enabled = true
}: UsePullToRefreshOptions) {
  const [state, setState] = useState<PullToRefreshState>({
    isPulling: false,
    pullDistance: 0,
    isRefreshing: false,
    canRefresh: false
  })

  const touchStartY = useRef<number>(0)
  const touchCurrentY = useRef<number>(0)
  const containerRef = useRef<HTMLElement | null>(null)

  useEffect(() => {
    if (!enabled) return

    let isAtTop = true
    let startY = 0
    let currentY = 0
    let rafId: number | null = null

    const checkScrollPosition = () => {
      if (!containerRef.current) return
      
      // Check if we're at the top of the scrollable container
      const scrollTop = containerRef.current.scrollTop || window.pageYOffset || document.documentElement.scrollTop
      isAtTop = scrollTop <= 0
    }

    const updatePullDistance = () => {
      if (!enabled || state.isRefreshing) return

      const distance = Math.max(0, (currentY - startY) / resistance)
      const canRefresh = distance >= threshold

      setState(prev => ({
        ...prev,
        pullDistance: distance,
        canRefresh
      }))

      rafId = null
    }

    const handleTouchStart = (e: TouchEvent) => {
      if (!enabled || state.isRefreshing) return
      
      checkScrollPosition()
      if (!isAtTop) return

      startY = e.touches[0].clientY
      touchStartY.current = startY
      
      setState(prev => ({ ...prev, isPulling: false }))
    }

    const handleTouchMove = (e: TouchEvent) => {
      if (!enabled || state.isRefreshing || !isAtTop) return

      currentY = e.touches[0].clientY
      touchCurrentY.current = currentY
      
      const deltaY = currentY - startY
      
      if (deltaY > 0) {
        // Prevent default scrolling when pulling down
        e.preventDefault()
        
        setState(prev => ({ ...prev, isPulling: true }))
        
        // Use RAF for smooth updates
        if (rafId === null) {
          rafId = requestAnimationFrame(updatePullDistance)
        }
      }
    }

    const handleTouchEnd = async () => {
      if (!enabled || !state.isPulling) return

      if (state.canRefresh && !state.isRefreshing) {
        setState(prev => ({ 
          ...prev, 
          isRefreshing: true,
          pullDistance: threshold // Keep at threshold during refresh
        }))

        try {
          await onRefresh()
        } catch (error) {
          console.error('Pull to refresh error:', error)
        } finally {
          // Reset state after refresh
          setState({
            isPulling: false,
            pullDistance: 0,
            isRefreshing: false,
            canRefresh: false
          })
        }
      } else {
        // Reset if not refreshing
        setState(prev => ({
          ...prev,
          isPulling: false,
          pullDistance: 0,
          canRefresh: false
        }))
      }

      if (rafId !== null) {
        cancelAnimationFrame(rafId)
        rafId = null
      }
    }

    // Add passive listeners for better performance
    const options: AddEventListenerOptions = { passive: false }
    
    document.addEventListener('touchstart', handleTouchStart, options)
    document.addEventListener('touchmove', handleTouchMove, options)
    document.addEventListener('touchend', handleTouchEnd, options)
    document.addEventListener('touchcancel', handleTouchEnd, options)

    return () => {
      document.removeEventListener('touchstart', handleTouchStart)
      document.removeEventListener('touchmove', handleTouchMove)
      document.removeEventListener('touchend', handleTouchEnd)
      document.removeEventListener('touchcancel', handleTouchEnd)
      
      if (rafId !== null) {
        cancelAnimationFrame(rafId)
      }
    }
  }, [enabled, state.isRefreshing, state.isPulling, state.canRefresh, threshold, resistance, onRefresh])

  return {
    ...state,
    containerRef,
    // Helper to set container reference
    setContainer: (element: HTMLElement | null) => {
      containerRef.current = element
    }
  }
}