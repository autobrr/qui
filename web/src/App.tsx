/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ThemeValidator } from "@/components/themes/ThemeValidator"
import { Toaster } from "@/components/ui/sonner"
import { useDynamicFavicon } from "@/hooks/useDynamicFavicon"
import { initializePWANativeTheme } from "@/utils/pwaNativeTheme"
import { initializeTheme } from "@/utils/theme"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { RouterProvider } from "@tanstack/react-router"
import { useEffect } from "react"
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

  useEffect(() => {
    initializeTheme().catch(console.error)
    initializePWANativeTheme()

    if (import.meta.env.PROD) {
      setupPWAAutoUpdate()
    }
  }, [])

  return (
    <QueryClientProvider client={queryClient}>
      <ThemeValidator />
      <RouterProvider router={router} />
      <Toaster />
    </QueryClientProvider>
  )
}

export default App
