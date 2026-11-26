/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Outlet, useLocation } from "@tanstack/react-router"
import { useReturnFocus } from "@/components/kit/use-return-focus"
import { CommandPalette } from "@/components/command-palette/CommandPalette"
import { MobileFooterNav } from "@/components/layout/MobileFooterNav"
import { Header } from "@/components/layout/Header"
import { Sidebar } from "@/components/layout/Sidebar"
import { ProblemReportDialogRoot } from "@/components/problem-reports/ProblemReportDialog"
import { SupportDialogRoot } from "@/components/support/SupportDialog"
import { ReminderBar } from "@/components/support/SupportReminder"
import { LayoutRouteProvider, useLayoutRoute } from "@/contexts/LayoutRouteContext"
import { usePersistedSidebarHidden, usePersistedSidebarIconOnly } from "@/hooks/usePersistedSidebarState"
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet"
import { useRef, useState, type MouseEvent } from "react"
import { useTranslation } from "react-i18next"
import { useSpreadsheetDisguise } from "@/lib/spreadsheet-disguise"
import { cn } from "@/lib/utils"
import { MobileScrollProvider, useMobileScroll } from "@/contexts/MobileScrollContext"
import { TorrentSelectionProvider } from "@/contexts/TorrentSelectionContext"
import { BulkActionProvider } from "@/components/torrents/bulk-actions/BulkActionProvider"
import { useCustomThemes } from "@/hooks/useCustomThemes"
import { DatabaseShrinkProvider } from "@/components/database/DatabaseShrinkProvider"

type FlyoutState = { kind: "closed" } | { kind: "open"; pathname: string } | { kind: "pinning" }

const closedFlyout: FlyoutState = { kind: "closed" }

function opensInPlace(event: MouseEvent): boolean {
  const link = (event.target as Element).closest("a[href]")
  return link !== null && link.getAttribute("target") !== "_blank" && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey
}

function AppLayoutContent() {
  const { t } = useTranslation("common")
  const [sidebarIconOnly, setSidebarIconOnly] = usePersistedSidebarIconOnly()
  const [sidebarHidden, setSidebarHidden] = usePersistedSidebarHidden()
  const pathname = useLocation({ select: (location) => location.pathname })
  const [flyout, setFlyout] = useState<FlyoutState>(closedFlyout)
  if (flyout.kind === "open" && (!sidebarHidden || flyout.pathname !== pathname)) setFlyout(closedFlyout)
  const flyoutOpen = flyout.kind === "open"
  const dockedSidebarRef = useRef<HTMLDivElement>(null)
  const openSidebarButtonRef = useRef<HTMLButtonElement>(null)
  const returnFocus = useReturnFocus(openSidebarButtonRef)
  const pinFlyout = () => {
    setFlyout({ kind: "pinning" })
    setSidebarIconOnly(false)
    setSidebarHidden(false)
    requestAnimationFrame(() => requestAnimationFrame(() => {
      setFlyout(closedFlyout)
      dockedSidebarRef.current?.querySelector("button")?.focus()
    }))
  }
  const { isFooterVisible } = useMobileScroll()
  const isInstanceRoute = useLayoutRoute().state.instanceId !== null
  const spreadsheetDisguise = useSpreadsheetDisguise()
  const topBarShowsSideways = isInstanceRoute || spreadsheetDisguise

  return (
    <div className={cn("flex h-[calc(100dvh-var(--app-inset-top))] bg-background md:pt-(--app-fade-gap)", !topBarShowsSideways && "sideways:pt-0")}>
      <div
        ref={dockedSidebarRef}
        inert={sidebarHidden}
        className={cn(
          "hidden lg:flex flex-shrink-0 overflow-hidden transition-[width] duration-300 ease-out motion-reduce:transition-none",
          sidebarHidden ? "w-0 overflow-clip" : sidebarIconOnly ? "w-16" : "w-64",
          flyout.kind === "pinning" && "transition-none [&_*]:transition-none!"
        )}
      >
        <div className={cn("h-full w-full", sidebarHidden && (sidebarIconOnly ? "min-w-16" : "min-w-64"))}>
          <Sidebar
            iconOnly={sidebarIconOnly}
            onToggleIconOnly={() => setSidebarIconOnly(!sidebarIconOnly)}
            onHide={() => {
              setFlyout(closedFlyout)
              setSidebarHidden(true)
              requestAnimationFrame(() => requestAnimationFrame(() => openSidebarButtonRef.current?.focus()))
            }}
          />
        </div>
      </div>

      {sidebarHidden && (
        <Sheet open={flyoutOpen} onOpenChange={(open) => setFlyout(open ? { kind: "open", pathname } : closedFlyout)}>
          <SheetContent
            side="left"
            hideClose
            aria-describedby={undefined}
            className="w-64 gap-0 border-r-0 p-0 shadow-xl outline-none motion-reduce:animate-none!"
            overlayClassName="bg-black/20 motion-reduce:animate-none!"
            onClickCapture={(event) => {
              if (opensInPlace(event)) setFlyout(closedFlyout)
            }}
            onOpenAutoFocus={(event) => {
              returnFocus.remember()
              event.preventDefault()
              const content = event.currentTarget as HTMLElement
              content.focus()
            }}
            onCloseAutoFocus={returnFocus.restore}
          >
            <SheetTitle className="sr-only">{t("sidebar.menu")}</SheetTitle>
            <Sidebar flyout iconOnly={false} onPin={pinFlyout} />
          </SheetContent>
        </Sheet>
      )}

      <div className="flex flex-1 flex-col min-w-0 relative">
        <Header sidebarIconOnly={sidebarIconOnly} sidebarHidden={sidebarHidden} sidebarOpen={flyoutOpen} openSidebarButtonRef={openSidebarButtonRef} onOpenSidebar={() => setFlyout({ kind: "open", pathname })} />
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

      <MobileFooterNav />

      <CommandPalette />
      <SupportDialogRoot />
      <ProblemReportDialogRoot />
    </div>
  )
}

export function AppLayout() {
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
