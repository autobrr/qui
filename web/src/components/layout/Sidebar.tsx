/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Link, useLocation } from "@tanstack/react-router"
import { cn } from "@/lib/utils"
import { 
  Home, 
  Server, 
  Settings, 
  LogOut,
  HardDrive,
  Github
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { useAuth } from "@/hooks/useAuth"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"

interface NavItem {
  title: string
  href: string
  icon: React.ComponentType<{ className?: string }>
}

const navigation: NavItem[] = [
  {
    title: "Dashboard",
    href: "/dashboard",
    icon: Home,
  },
  {
    title: "Instances",
    href: "/instances",
    icon: Server,
  },
  {
    title: "Settings",
    href: "/settings",
    icon: Settings,
  },
]

export function Sidebar() {
  const location = useLocation()
  const { logout } = useAuth()
  
  const { data: instances } = useQuery({
    queryKey: ["instances"],
    queryFn: () => api.getInstances(),
  })

  return (
    <div className="flex h-full w-64 flex-col border-r bg-sidebar border-sidebar-border">
      <div className="p-6">
        <h2 className="text-lg font-semibold text-sidebar-foreground">qui</h2>
      </div>
      
      <nav className="flex-1 space-y-1 px-3">
        {navigation.map((item) => {
          const Icon = item.icon
          const isActive = location.pathname === item.href
          
          return (
            <Link
              key={item.href}
              to={item.href}
              className={cn(
                "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-all duration-200 ease-out",
                isActive? "bg-sidebar-primary text-sidebar-primary-foreground": "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
              )}
            >
              <Icon className="h-4 w-4" />
              {item.title}
            </Link>
          )
        })}
        
        <Separator className="my-4" />
        
        <div className="space-y-1">
          <p className="px-3 text-xs font-semibold text-sidebar-foreground/70 uppercase tracking-wider">
            Instances
          </p>
          {instances?.map((instance) => {
            const instancePath = `/instances/${instance.id}`
            const isActive = location.pathname === instancePath
            
            return (
              <Link
                key={instance.id}
                to="/instances/$instanceId"
                params={{ instanceId: instance.id.toString() }}
                className={cn(
                  "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-all duration-200 ease-out",
                  isActive? "bg-sidebar-primary text-sidebar-primary-foreground": "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
                )}
              >
                <HardDrive className="h-4 w-4" />
                <span className="truncate">{instance.name}</span>
                <span
                  className={cn(
                    "ml-auto h-2 w-2 rounded-full",
                    instance.connected ? "bg-green-500" : "bg-red-500"
                  )}
                />
              </Link>
            )
          })}
          {(!instances || instances.length === 0) && (
            <p className="px-3 py-2 text-sm text-sidebar-foreground/50">
              No instances configured
            </p>
          )}
        </div>
      </nav>
      
      <div className="mt-auto space-y-3 p-3">
        <Button
          variant="ghost"
          className="w-full justify-start"
          onClick={() => logout()}
        >
          <LogOut className="mr-2 h-4 w-4" />
          Logout
        </Button>
        
        <Separator className="mx-3 mb-3" />
        
        <div className="flex items-center justify-between px-3 pb-3">
          <p className="text-[10px] text-sidebar-foreground/40">
            © {new Date().getFullYear() > 2025 ? `2025-${new Date().getFullYear()}` : new Date().getFullYear()} autobrr • GPL-2.0-or-later
          </p>
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6 text-sidebar-foreground/40 hover:text-sidebar-foreground"
            asChild
          >
            <a 
              href="https://github.com/autobrr/qui" 
              target="_blank" 
              rel="noopener noreferrer"
              aria-label="View on GitHub"
            >
              <Github className="h-3.5 w-3.5" />
            </a>
          </Button>
        </div>
      </div>
    </div>
  )
}