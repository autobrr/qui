/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { BuiltinThemesLoader } from "@/components/themes/BuiltinThemesLoader"
import { DiscScanToasts } from "@/components/torrents/DiscScanToasts"
import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"
import { SyncStreamProvider } from "@/contexts/SyncStreamContext"
import { useDynamicFavicon } from "@/hooks/useDynamicFavicon"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { RouterProvider } from "@tanstack/react-router"
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

  return (
    <QueryClientProvider client={queryClient}>
      <SyncStreamProvider>
        <BuiltinThemesLoader />
        <DiscScanToasts />
        <TooltipProvider>
          <RouterProvider router={router} />
          <Toaster />
        </TooltipProvider>
      </SyncStreamProvider>
    </QueryClientProvider>
  )
}

export default App
