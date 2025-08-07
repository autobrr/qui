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
import { Download, Smartphone, Monitor, X } from 'lucide-react'

interface BeforeInstallPromptEvent extends Event {
  readonly platforms: string[]
  readonly userChoice: Promise<{
    outcome: 'accepted' | 'dismissed'
    platform: string
  }>
  prompt(): Promise<void>
}

interface InstallPromptProps {
  onInstall?: () => void
  onDismiss?: () => void
}

export function InstallPrompt({ onInstall, onDismiss }: InstallPromptProps) {
  const [deferredPrompt, setDeferredPrompt] = useState<BeforeInstallPromptEvent | null>(null)
  const [showInstallPrompt, setShowInstallPrompt] = useState(false)
  const [isInstallable, setIsInstallable] = useState(false)

  useEffect(() => {
    const handler = (e: Event) => {
      e.preventDefault()
      const promptEvent = e as BeforeInstallPromptEvent
      setDeferredPrompt(promptEvent)
      setIsInstallable(true)
      
      // Show our custom prompt after a short delay
      setTimeout(() => {
        setShowInstallPrompt(true)
      }, 3000) // Wait 3 seconds before showing
    }

    window.addEventListener('beforeinstallprompt', handler)

    // Check if app is already installed
    const isStandalone = window.matchMedia('(display-mode: standalone)').matches
    const isIOSStandalone = (window.navigator as any).standalone === true
    
    if (isStandalone || isIOSStandalone) {
      setIsInstallable(false)
    }

    return () => {
      window.removeEventListener('beforeinstallprompt', handler)
    }
  }, [])

  const handleInstall = async () => {
    if (!deferredPrompt) return

    try {
      await deferredPrompt.prompt()
      const { outcome } = await deferredPrompt.userChoice
      
      if (outcome === 'accepted') {
        onInstall?.()
      }
      
      setDeferredPrompt(null)
      setShowInstallPrompt(false)
    } catch (error) {
      console.error('Install prompt error:', error)
    }
  }

  const handleDismiss = () => {
    setShowInstallPrompt(false)
    setDeferredPrompt(null)
    onDismiss?.()
    
    // Don't show again for this session
    setIsInstallable(false)
  }

  if (!isInstallable || !showInstallPrompt) {
    return null
  }

  return (
    <Dialog open={showInstallPrompt} onOpenChange={setShowInstallPrompt}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <div className="flex items-center gap-3 mb-2">
            <div className="p-2 rounded-lg bg-primary/10">
              <Download className="h-6 w-6 text-primary" />
            </div>
            <DialogTitle className="text-left">Install qui</DialogTitle>
          </div>
          <DialogDescription className="text-left">
            Install this app on your device for a native-like experience.
          </DialogDescription>
        </DialogHeader>
        
        <div className="flex flex-col gap-3 my-4">
          <div className="flex items-center gap-3 p-3 rounded-lg bg-muted/30">
            <Smartphone className="h-5 w-5 text-muted-foreground" />
            <div className="flex-1">
              <p className="text-sm font-medium">Quick Access</p>
              <p className="text-xs text-muted-foreground">Launch from your home screen</p>
            </div>
          </div>
          <div className="flex items-center gap-3 p-3 rounded-lg bg-muted/30">
            <Monitor className="h-5 w-5 text-muted-foreground" />
            <div className="flex-1">
              <p className="text-sm font-medium">Native Experience</p>
              <p className="text-xs text-muted-foreground">Works offline and loads faster</p>
            </div>
          </div>
        </div>

        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={handleDismiss} className="flex-1">
            <X className="h-4 w-4 mr-2" />
            Not Now
          </Button>
          <Button onClick={handleInstall} className="flex-1">
            <Download className="h-4 w-4 mr-2" />
            Install
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Hook for manual install trigger
export function useInstallPrompt() {
  const [deferredPrompt, setDeferredPrompt] = useState<BeforeInstallPromptEvent | null>(null)
  const [isInstallable, setIsInstallable] = useState(false)

  useEffect(() => {
    const handler = (e: Event) => {
      e.preventDefault()
      const promptEvent = e as BeforeInstallPromptEvent
      setDeferredPrompt(promptEvent)
      setIsInstallable(true)
    }

    window.addEventListener('beforeinstallprompt', handler)

    // Check if app is already installed
    const isStandalone = window.matchMedia('(display-mode: standalone)').matches
    const isIOSStandalone = (window.navigator as any).standalone === true
    
    if (isStandalone || isIOSStandalone) {
      setIsInstallable(false)
    }

    return () => {
      window.removeEventListener('beforeinstallprompt', handler)
    }
  }, [])

  const install = async () => {
    if (!deferredPrompt) return false

    try {
      await deferredPrompt.prompt()
      const { outcome } = await deferredPrompt.userChoice
      
      setDeferredPrompt(null)
      setIsInstallable(false)
      
      return outcome === 'accepted'
    } catch (error) {
      console.error('Install prompt error:', error)
      return false
    }
  }

  return {
    isInstallable,
    install
  }
}