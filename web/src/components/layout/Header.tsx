import { useAuth } from '@/hooks/useAuth'
import { Button } from '@/components/ui/button'
import { 
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { User, LogOut, Key } from 'lucide-react'
import { ThemeToggle } from '@/components/ui/ThemeToggle'
import { cn } from '@/lib/utils'
import { Link } from '@tanstack/react-router'

interface HeaderProps {
  children?: React.ReactNode
  sidebarCollapsed?: boolean
}

export function Header({ children, sidebarCollapsed = false }: HeaderProps) {
  const { user, logout } = useAuth()

  return (
    <header className="sticky top-0 z-50 flex h-16 items-center justify-between sm:border-b bg-background pl-1 pr-4 sm:pr-6 lg:static">
      <div className="flex items-center gap-1">
        {children}
        <h1 className={cn(
          "text-xl font-semibold transition-opacity duration-300",
          "lg:opacity-0 lg:pointer-events-none", // Hidden on desktop by default
          sidebarCollapsed && "lg:opacity-100 lg:pointer-events-auto" // Visible on desktop when sidebar collapsed
        )}>qui</h1>
      </div>
      
      <div className="flex items-center gap-3">
        <ThemeToggle />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="sm" className="gap-2">
              <User className="h-4 w-4" />
              <span className="hidden sm:inline">{user?.username}</span>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuLabel>My Account</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild>
              <Link to="/settings" search={{ tab: 'api' }} className="flex cursor-pointer">
                <Key className="mr-2 h-4 w-4" />
                API Keys
              </Link>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => logout()}>
              <LogOut className="mr-2 h-4 w-4" />
              Logout
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  )
}