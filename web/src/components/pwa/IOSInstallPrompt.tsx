import { useState, useEffect } from 'react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Share, Plus, X } from 'lucide-react'

export function IOSInstallPrompt() {
  const [showPrompt, setShowPrompt] = useState(false)
  const [isIOS, setIsIOS] = useState(false)

  useEffect(() => {
    // Detect iOS Safari
    const userAgent = window.navigator.userAgent
    const isIOSDevice = /iPad|iPhone|iPod/.test(userAgent)
    const isSafari = /Safari/.test(userAgent) && !/Chrome/.test(userAgent)
    
    setIsIOS(isIOSDevice && isSafari)

    // Check if already installed
    const isStandalone = window.matchMedia('(display-mode: standalone)').matches
    const isIOSStandalone = (window.navigator as any).standalone === true
    
    if (isIOSDevice && isSafari && !isStandalone && !isIOSStandalone) {
      // Show iOS install prompt after delay
      const timer = setTimeout(() => {
        setShowPrompt(true)
      }, 3000)

      return () => clearTimeout(timer)
    }
  }, [])

  const handleDismiss = () => {
    setShowPrompt(false)
    // Remember dismissal for session
    sessionStorage.setItem('ios-install-dismissed', 'true')
  }

  // Don't show if previously dismissed this session
  if (sessionStorage.getItem('ios-install-dismissed') === 'true') {
    return null
  }

  if (!isIOS || !showPrompt) {
    return null
  }

  return (
    <Dialog open={showPrompt} onOpenChange={setShowPrompt}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Share className="h-5 w-5" />
            Install qui
          </DialogTitle>
          <DialogDescription>
            Add this app to your home screen for quick access and a native experience.
          </DialogDescription>
        </DialogHeader>
        
        <div className="space-y-4 my-4">
          <div className="flex items-start gap-3 p-3 rounded-lg bg-muted/30">
            <div className="flex-shrink-0 w-6 h-6 bg-blue-500 text-white rounded-full flex items-center justify-center text-sm font-bold">
              1
            </div>
            <div>
              <p className="text-sm font-medium">Tap the Share button</p>
              <p className="text-xs text-muted-foreground">Look for the <Share className="inline h-3 w-3 mx-1" /> icon in Safari's toolbar</p>
            </div>
          </div>
          
          <div className="flex items-start gap-3 p-3 rounded-lg bg-muted/30">
            <div className="flex-shrink-0 w-6 h-6 bg-blue-500 text-white rounded-full flex items-center justify-center text-sm font-bold">
              2
            </div>
            <div>
              <p className="text-sm font-medium">Select "Add to Home Screen"</p>
              <p className="text-xs text-muted-foreground">Scroll down to find <Plus className="inline h-3 w-3 mx-1" /> "Add to Home Screen"</p>
            </div>
          </div>
          
          <div className="flex items-start gap-3 p-3 rounded-lg bg-muted/30">
            <div className="flex-shrink-0 w-6 h-6 bg-blue-500 text-white rounded-full flex items-center justify-center text-sm font-bold">
              3
            </div>
            <div>
              <p className="text-sm font-medium">Tap "Add"</p>
              <p className="text-xs text-muted-foreground">Confirm to add the app to your home screen</p>
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={handleDismiss} className="flex-1">
            <X className="h-4 w-4 mr-2" />
            Maybe Later
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}