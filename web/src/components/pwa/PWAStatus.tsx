import { useState, useEffect } from 'react'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Download, Wifi, WifiOff } from 'lucide-react'
import { useInstallPrompt } from './InstallPrompt'

export function PWAStatus() {
  const [isOnline, setIsOnline] = useState(navigator.onLine)
  const [isStandalone, setIsStandalone] = useState(false)
  const { isInstallable, install } = useInstallPrompt()

  useEffect(() => {
    // Check if running as PWA
    const standalone = window.matchMedia('(display-mode: standalone)').matches
    const iosStandalone = (window.navigator as any).standalone === true
    setIsStandalone(standalone || iosStandalone)

    // Online/offline status
    const handleOnline = () => setIsOnline(true)
    const handleOffline = () => setIsOnline(false)

    window.addEventListener('online', handleOnline)
    window.addEventListener('offline', handleOffline)

    return () => {
      window.removeEventListener('online', handleOnline)
      window.removeEventListener('offline', handleOffline)
    }
  }, [])

  const handleInstall = async () => {
    const success = await install()
    if (success) {
      console.log('PWA installed successfully')
    }
  }

  return (
    <div className="flex items-center gap-2">
      {/* Online/Offline Status - Mobile Only */}
      <div className="sm:hidden">
        <Tooltip>
          <TooltipTrigger asChild>
            <div className="flex items-center">
              {isOnline ? (
                <Wifi className="h-4 w-4 text-primary" />
              ) : (
                <WifiOff className="h-4 w-4 text-destructive" />
              )}
            </div>
          </TooltipTrigger>
          <TooltipContent>
            {isOnline ? 'Online' : 'Offline - Using cached data'}
          </TooltipContent>
        </Tooltip>
      </div>

      {/* Install Button */}
      {isInstallable && !isStandalone && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              onClick={handleInstall}
              className="h-8 px-2"
            >
              <Download className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            Install as app
          </TooltipContent>
        </Tooltip>
      )}
    </div>
  )
}