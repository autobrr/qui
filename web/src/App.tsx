/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

import { RouterProvider } from '@tanstack/react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { router } from './router'
import { useEffect } from 'react'
import { initializeTheme } from '@/utils/theme'
import { initializePWANativeTheme } from '@/utils/pwaNativeTheme'
import { Toaster } from '@/components/ui/sonner'
import { InstallPrompt } from '@/components/pwa/InstallPrompt'
import { IOSInstallPrompt } from '@/components/pwa/IOSInstallPrompt'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5 * 1000,
      refetchOnWindowFocus: false,
    },
  },
})

function App() {
  useEffect(() => {
    initializeTheme().catch(console.error)
    initializePWANativeTheme()
  }, [])
  
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
      <Toaster />
      <InstallPrompt />
      <IOSInstallPrompt />
    </QueryClientProvider>
  )
}

export default App
