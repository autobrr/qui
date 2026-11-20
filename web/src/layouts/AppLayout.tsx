/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Outlet } from "@tanstack/react-router"
import { CommandPalette } from "@/components/command-palette/CommandPalette"
import { MobileFooterNav } from "@/components/layout/MobileFooterNav"
import { Header } from "@/components/layout/Header"
import { Sidebar } from "@/components/layout/Sidebar"
import { ProblemReportDialogRoot } from "@/components/problem-reports/ProblemReportDialog"
import { SupportDialogRoot } from "@/components/support/SupportDialog"
import { ReminderBar } from "@/components/support/SupportReminder"
import { LayoutRouteProvider, useLayoutRoute } from "@/contexts/LayoutRouteContext"
import { usePersistedSidebarIconOnly } from "@/hooks/usePersistedSidebarState"
import { useSpreadsheetDisguise } from "@/lib/spreadsheet-disguise"
import { cn } from "@/lib/utils"
import { MobileScrollProvider, useMobileScroll } from "@/contexts/MobileScrollContext"
import { TorrentSelectionProvider } from "@/contexts/TorrentSelectionContext"
import { BulkActionProvider } from "@/components/torrents/bulk-actions/BulkActionProvider"
import { useCustomThemes } from "@/hooks/useCustomThemes"
import { DatabaseShrinkProvider } from "@/components/database/DatabaseShrinkProvider"

function AppLayoutContent() {
  const [sidebarIconOnly, setSidebarIconOnly] = usePersistedSidebarIconOnly()
  const { isFooterVisible } = useMobileScroll()
  const isInstanceRoute = useLayoutRoute().state.instanceId !== null
  const spreadsheetDisguise = useSpreadsheetDisguise()
  const topBarShowsSideways = isInstanceRoute || spreadsheetDisguise

  return (
    // Where no top bar shows (the classes of Header and SpreadsheetRibbonTabs), the fade gap goes inside main instead, so it scrolls away with the page.
    // Off an instance route, each page has 1rem of top padding of its own, so main adds a smaller part of the gap.
    <div className={cn("flex h-[calc(100dvh-var(--app-inset-top))] bg-background md:pt-(--app-fade-gap)", !topBarShowsSideways && "sideways:pt-0")}>
      <div
        className={cn(
          "hidden lg:flex flex-shrink-0 overflow-hidden transition-[width] duration-300 ease-out motion-reduce:transition-none",
          sidebarIconOnly ? "w-16" : "w-64"
        )}
      >
        <Sidebar iconOnly={sidebarIconOnly} onToggleIconOnly={() => setSidebarIconOnly(!sidebarIconOnly)} />
      </div>

      <div className="flex flex-1 flex-col min-w-0 relative">
        <Header sidebarIconOnly={sidebarIconOnly} />
        <ReminderBar />
        <main className={cn(
          "flex-1 overflow-y-auto",
          isInstanceRoute ? "max-md:pt-(--app-fade-gap)" : "max-md:pt-[max(0px,var(--app-fade-gap)_-_0.5rem)]",
          !topBarShowsSideways && "sideways:pt-[max(0px,var(--app-fade-gap)_-_0.5rem)]",
          isFooterVisible ? "pb-[calc(4rem+env(safe-area-inset-bottom))]" : "pb-0",
          "lg:pb-0"
        )}>
          <Outlet />
        </main>
      </div>

      {/* Mobile Footer Navigation */}
      <MobileFooterNav />

      <CommandPalette />
      <SupportDialogRoot />
      <ProblemReportDialogRoot />
    </div>
  )
}

export function AppLayout() {
  // Registers and applies stored custom themes for authenticated users.
  useCustomThemes()
  return (
    <LayoutRouteProvider>
      <TorrentSelectionProvider>
        <BulkActionProvider>
          <MobileScrollProvider>
            <DatabaseShrinkProvider>
              <AppLayoutContent />
            </DatabaseShrinkProvider>
          </MobileScrollProvider>
        </BulkActionProvider>
      </TorrentSelectionProvider>
    </LayoutRouteProvider>
  )
}
