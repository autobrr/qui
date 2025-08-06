import { RefreshCw, ArrowDown } from 'lucide-react'
import { cn } from '@/lib/utils'

interface PullToRefreshIndicatorProps {
  pullDistance: number
  threshold: number
  isRefreshing: boolean
  canRefresh: boolean
  isPulling: boolean
}

export function PullToRefreshIndicator({
  pullDistance,
  threshold,
  isRefreshing,
  canRefresh,
  isPulling
}: PullToRefreshIndicatorProps) {
  // Calculate progress (0 to 1)
  const progress = Math.min(pullDistance / threshold, 1)
  const rotation = progress * 360

  // Don't show if not pulling or refreshing
  if (!isPulling && !isRefreshing) return null

  return (
    <div 
      className="fixed top-0 left-0 right-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm border-b transition-all duration-200"
      style={{
        transform: `translateY(${Math.max(0, pullDistance - threshold)}px)`,
        opacity: isPulling || isRefreshing ? 1 : 0,
        height: '60px'
      }}
    >
      <div className="flex items-center gap-2 px-4 py-2 rounded-full bg-background border shadow-sm">
        {isRefreshing ? (
          <>
            <RefreshCw className={cn(
              "h-5 w-5 text-primary animate-spin"
            )} />
            <span className="text-sm font-medium text-foreground">
              Refreshing...
            </span>
          </>
        ) : (
          <>
            <div className="relative">
              {canRefresh ? (
                <RefreshCw className="h-5 w-5 text-primary" />
              ) : (
                <ArrowDown 
                  className={cn(
                    "h-5 w-5 transition-all duration-200",
                    canRefresh ? "text-primary" : "text-muted-foreground"
                  )}
                  style={{
                    transform: `rotate(${rotation}deg)`,
                    opacity: Math.max(0.3, progress)
                  }}
                />
              )}
            </div>
            <span className="text-sm font-medium text-foreground">
              {canRefresh ? 'Release to refresh' : 'Pull to refresh'}
            </span>
          </>
        )}
      </div>
      
      {/* Progress bar */}
      <div className="absolute bottom-0 left-0 right-0 h-0.5 bg-muted">
        <div 
          className="h-full bg-primary transition-all duration-100"
          style={{ width: `${progress * 100}%` }}
        />
      </div>
    </div>
  )
}