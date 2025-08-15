/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Link, useLocation } from '@tanstack/react-router'
import { cn } from '@/lib/utils'
import { 
  Home, 
  Settings,
  HardDrive,
  Server,
  Github,
  LogOut
} from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useAuth } from '@/hooks/useAuth'
import { Badge } from '@/components/ui/badge'
import { useTorrentSelection } from '@/contexts/TorrentSelectionContext'


export function MobileFooterNav() {
  const location = useLocation()
  const { logout } = useAuth()
  const { isSelectionMode } = useTorrentSelection()
  
  const { data: instances } = useQuery({
    queryKey: ['instances'],
    queryFn: () => api.getInstances(),
  })

  const activeInstances = instances?.filter(i => i.connected) || []
  const isOnInstancePage = location.pathname.startsWith('/instances/')
  const currentInstanceId = isOnInstancePage 
    ? location.pathname.split('/')[2] 
    : null
  const currentInstance = instances?.find(i => i.id.toString() === currentInstanceId)

  if (isSelectionMode) {
    return null
  }

  return (
    <nav 
      className={cn(
        "fixed bottom-0 left-0 right-0 z-40 lg:hidden",
        "bg-background/80 backdrop-blur-md border-t border-border/50"
      )}
      style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
    >
      <div className="flex items-center justify-around h-16">
        {/* Dashboard */}
        <Link
          to="/dashboard"
          className={cn(
            'flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1',
            location.pathname === '/dashboard'
              ? 'text-primary'
              : 'text-muted-foreground hover:text-foreground'
          )}
        >
          <Home className={cn(
            "h-5 w-5",
            location.pathname === '/dashboard' && "text-primary"
          )} />
          <span className="truncate">Dashboard</span>
        </Link>

        {/* Clients dropdown */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              className={cn(
                'flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 hover:cursor-pointer',
                isOnInstancePage
                  ? 'text-primary'
                  : 'text-muted-foreground hover:text-foreground'
              )}
            >
              <div className="relative">
                <HardDrive className={cn(
                  "h-5 w-5",
                  isOnInstancePage && "text-primary"
                )} />
                {activeInstances.length > 0 && (
                  <Badge 
                    className="absolute -top-1 -right-2 h-4 w-4 p-0 flex items-center justify-center text-[9px]"
                    variant="default"
                  >
                    {activeInstances.length}
                  </Badge>
                )}
              </div>
              <span className="truncate">
                {currentInstance ? currentInstance.name : 'Clients'}
              </span>
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="center" side="top" className="w-56 mb-2">
            <DropdownMenuLabel>qBittorrent Clients</DropdownMenuLabel>
            <DropdownMenuSeparator />
            {instances?.map((instance) => (
              <DropdownMenuItem key={instance.id} asChild>
                <Link
                  to="/instances/$instanceId"
                  params={{ instanceId: instance.id.toString() }}
                  className="flex items-center gap-2"
                >
                  <HardDrive className="h-4 w-4" />
                  <span className="flex-1 truncate">{instance.name}</span>
                  <span
                    className={cn(
                      "h-2 w-2 rounded-full",
                      instance.connected ? "bg-green-500" : "bg-red-500"
                    )}
                  />
                </Link>
              </DropdownMenuItem>
            ))}
            {(!instances || instances.length === 0) && (
              <DropdownMenuItem disabled>
                No clients configured
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Settings dropdown */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              className={cn(
                'flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 hover:cursor-pointer',
                (location.pathname === '/settings' || location.pathname === '/instances')
                  ? 'text-primary'
                  : 'text-muted-foreground hover:text-foreground'
              )}
            >
              <Settings className={cn(
                "h-5 w-5",
                (location.pathname === '/settings' || location.pathname === '/instances') && "text-primary"
              )} />
              <span className="truncate">Settings</span>
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" side="top" className="mb-2">
            <DropdownMenuItem asChild>
              <Link 
                to="/settings"
                className="flex items-center gap-2"
              >
                <Settings className="h-4 w-4" />
                General Settings
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link 
                to="/instances"
                className="flex items-center gap-2"
              >
                <Server className="h-4 w-4" />
                Manage Instances
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <a 
                href="https://github.com/autobrr/qui" 
                target="_blank" 
                rel="noopener noreferrer"
                className="flex items-center gap-2"
              >
                <Github className="h-4 w-4" />
                GitHub
              </a>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem 
              onClick={() => logout()}
              className="text-destructive focus:text-destructive flex items-center gap-2"
            >
              <LogOut className="h-4 w-4 text-destructive" />
              Logout
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </nav>
  )
}