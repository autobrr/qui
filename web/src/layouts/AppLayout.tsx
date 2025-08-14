/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from 'react'
import { Outlet } from '@tanstack/react-router'
import { Sidebar } from '@/components/layout/Sidebar'
import { Header } from '@/components/layout/Header'
import { usePersistedSidebarState } from '@/hooks/usePersistedSidebarState'
import { Menu } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetTrigger, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { VisuallyHidden } from '@/components/ui/visually-hidden'
import { cn } from '@/lib/utils'

export function AppLayout() {
  const [sidebarOpen, setSidebarOpen] = useState(false) // Mobile: always starts closed
  const [sidebarCollapsed, setSidebarCollapsed] = usePersistedSidebarState(false) // Desktop: persisted state

  return (
    <div className="flex h-screen bg-background">
      {/* Desktop Sidebar - Collapsible */}
      <div className={cn(
        "hidden lg:flex transition-all duration-300 ease-out overflow-hidden",
        sidebarCollapsed ? "w-0 opacity-0" : "w-64 opacity-100"
      )}>
        <div className="w-64 flex-shrink-0">
          <Sidebar />
        </div>
      </div>
      
      <div className="flex flex-1 flex-col min-w-0">
        <Header sidebarCollapsed={sidebarCollapsed}>
          {/* Desktop toggle button */}
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setSidebarCollapsed(!sidebarCollapsed)}
            className="hidden lg:flex transition-transform duration-200 hover:scale-110"
          >
            <Menu className={cn(
              "h-5 w-5 transition-transform duration-300",
              sidebarCollapsed && "rotate-90"
            )} />
          </Button>
          
          {/* Mobile toggle button */}
          <Sheet open={sidebarOpen} onOpenChange={setSidebarOpen}>
            <SheetTrigger asChild className="lg:hidden">
              <Button variant="ghost" size="icon">
                <Menu className="h-5 w-5" />
              </Button>
            </SheetTrigger>
            <SheetContent side="left" className="p-0 w-64">
              <SheetHeader>
                <VisuallyHidden>
                  <SheetTitle>Navigation Menu</SheetTitle>
                </VisuallyHidden>
              </SheetHeader>
              <Sidebar onNavigate={() => setSidebarOpen(false)} />
            </SheetContent>
          </Sheet>
        </Header>
        <main className="flex-1 overflow-y-auto">
          <Outlet />
        </main>
      </div>
    </div>
  )
}