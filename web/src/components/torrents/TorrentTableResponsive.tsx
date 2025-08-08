import { useEffect, useState } from 'react'
import { TorrentTableOptimized } from './TorrentTableOptimized'
import { TorrentCardsMobile } from './TorrentCardsMobile'
import type { Torrent } from '@/types'

interface TorrentTableResponsiveProps {
  instanceId: number
  filters?: {
    status: string[]
    categories: string[]
    tags: string[]
    trackers: string[]
  }
  selectedTorrent?: Torrent | null
  onTorrentSelect?: (torrent: Torrent | null) => void
  addTorrentModalOpen?: boolean
  onAddTorrentModalChange?: (open: boolean) => void
  onFilteredDataUpdate?: (torrents: Torrent[], total: number, counts?: any, categories?: any, tags?: string[]) => void
  filterButton?: React.ReactNode
}

export function TorrentTableResponsive(props: TorrentTableResponsiveProps) {
  const [isMobile, setIsMobile] = useState(false)
  
  useEffect(() => {
    // Check initial size
    const checkMobile = () => {
      // Use 768px as breakpoint (md in Tailwind)
      setIsMobile(window.innerWidth < 768)
    }
    
    checkMobile()
    
    // Listen for resize events with debounce
    let timeoutId: NodeJS.Timeout
    const handleResize = () => {
      clearTimeout(timeoutId)
      timeoutId = setTimeout(checkMobile, 150)
    }
    
    window.addEventListener('resize', handleResize)
    
    // Also listen for orientation change on mobile devices
    window.addEventListener('orientationchange', checkMobile)
    
    return () => {
      window.removeEventListener('resize', handleResize)
      window.removeEventListener('orientationchange', checkMobile)
      clearTimeout(timeoutId)
    }
  }, [])
  
  // You can also use CSS media query for more accurate detection
  useEffect(() => {
    const mediaQuery = window.matchMedia('(max-width: 767px)')
    
    const handleChange = (e: MediaQueryListEvent) => {
      setIsMobile(e.matches)
    }
    
    // Set initial value
    setIsMobile(mediaQuery.matches)
    
    // Modern browsers
    if (mediaQuery.addEventListener) {
      mediaQuery.addEventListener('change', handleChange)
      return () => mediaQuery.removeEventListener('change', handleChange)
    } 
    // Legacy browsers
    else if (mediaQuery.addListener) {
      mediaQuery.addListener(handleChange)
      return () => mediaQuery.removeListener(handleChange)
    }
  }, [])
  
  // Render appropriate component based on screen size
  if (isMobile) {
    return <TorrentCardsMobile {...props} />
  }
  
  return <TorrentTableOptimized {...props} />
}