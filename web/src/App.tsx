/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { RouterProvider } from "@tanstack/react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { router } from "./router"
import { useEffect } from "react"
import { initializeTheme } from "@/utils/theme"
import { initializePWANativeTheme } from "@/utils/pwaNativeTheme"
import { Toaster } from "@/components/ui/sonner"
import { ThemeValidator } from "@/components/themes/ThemeValidator"

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
      <ThemeValidator />
      <RouterProvider router={router} />
      <Toaster />
    </QueryClientProvider>
  )
}

export default App
