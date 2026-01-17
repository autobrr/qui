/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"
import { PWAInstallPrompt } from "@/components/pwa/PWAInstallPrompt"
import { useDynamicFavicon } from "@/hooks/useDynamicFavicon"
import { initializePWANativeTheme } from "@/utils/pwaNativeTheme"
import { initializeTheme } from "@/utils/theme"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { RouterProvider } from "@tanstack/react-router"
import { useEffect } from "react"
import { useOfflineRedirect } from "@/hooks/useOfflineRedirect"
import { setupPWAAutoUpdate } from "./pwa"
import { router } from "./router"


const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5 * 1000,
      refetchOnWindowFocus: false,
    },
  },
})

function App() {
  useDynamicFavicon()
  useOfflineRedirect()

  useEffect(() => {
    initializeTheme().catch(console.error)
    initializePWANativeTheme()

    if (import.meta.env.PROD) {
      setupPWAAutoUpdate()
    }
  }, [])

  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={router} />
        <PWAInstallPrompt />
        <Toaster />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

export default App
