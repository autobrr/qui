import { useInstances } from '@/hooks/useInstances'
import { useInstanceStats } from '@/hooks/useInstanceStats'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import { Button } from '@/components/ui/button'
import { HardDrive, Download, Upload, AlertCircle, Activity, Plus, Zap, ChevronDown } from 'lucide-react'
import { Link } from '@tanstack/react-router'
import { useMemo } from 'react'
import { formatSpeed } from '@/lib/utils'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

// Custom hook to safely get all instance stats
function useAllInstanceStats(instances: any[]) {
  // Always call the same number of hooks by using a fixed array
  const maxInstances = 10 // Support up to 10 instances
  const fixedInstances = [...instances, ...Array(maxInstances - instances.length).fill(null)]
  
  const statsQueries = fixedInstances.map((instance) => 
    useInstanceStats(instance?.id || -1, { 
      enabled: instance !== null, 
      pollingInterval: 5000 
    })
  )
  
  return instances.map((instance, index) => ({
    instance,
    stats: statsQueries[index].data
  }))
}


function InstanceCard({ instance }: { instance: any }) {
  const { data: stats, isLoading, error } = useInstanceStats(instance.id, { 
    enabled: true, // Always fetch stats, regardless of isActive status
    pollingInterval: 5000 // Slower polling for dashboard
  })
  
  // Show loading only on first load
  if (isLoading && !stats) {
    return (
      <Link to="/instances/$instanceId" params={{ instanceId: instance.id.toString() }}>
        <Card className="hover:shadow-lg transition-shadow cursor-pointer opacity-60">
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="text-lg">{instance.name}</CardTitle>
              <Badge variant="secondary">
                Loading...
              </Badge>
            </div>
            <CardDescription>{instance.host}:{instance.port}</CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">Loading stats...</p>
          </CardContent>
        </Card>
      </Link>
    )
  }
  
  // If we have stats but instance is not connected, show with zero values
  if (stats && !stats.connected) {
    return (
      <Link to="/instances/$instanceId" params={{ instanceId: instance.id.toString() }}>
        <Card className="hover:shadow-lg transition-shadow cursor-pointer">
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="text-lg">{instance.name}</CardTitle>
              <Badge variant="destructive">Disconnected</Badge>
            </div>
            <CardDescription>{instance.host}:{instance.port}</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <p className="text-muted-foreground">Total</p>
                  <p className="font-semibold">0</p>
                </div>
                <div>
                  <p className="text-muted-foreground">Active</p>
                  <p className="font-semibold">0</p>
                </div>
              </div>
              <p className="text-sm text-muted-foreground text-center">
                Instance is disconnected
              </p>
            </div>
          </CardContent>
        </Card>
      </Link>
    )
  }
  
  // If we have an error or no stats data, show error state
  if (error || !stats || !stats.torrents) {
    return (
      <Link to="/instances/$instanceId" params={{ instanceId: instance.id.toString() }}>
        <Card className="hover:shadow-lg transition-shadow cursor-pointer opacity-60">
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="text-lg">{instance.name}</CardTitle>
              <Badge variant="destructive">Error</Badge>
            </div>
            <CardDescription>{instance.host}:{instance.port}</CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">
              Failed to load stats
            </p>
          </CardContent>
        </Card>
      </Link>
    )
  }
  
  return (
    <Link to="/instances/$instanceId" params={{ instanceId: instance.id.toString() }}>
      <Card className="hover:shadow-lg transition-shadow cursor-pointer">
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="text-lg">{instance.name}</CardTitle>
            <Badge variant={stats.connected ? 'default' : 'destructive'}>
              {stats.connected ? 'Connected' : 'Disconnected'}
            </Badge>
          </div>
          <CardDescription className="text-xs">{instance.host}:{instance.port}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            <div>
              <div className="flex items-center justify-between mb-1">
                <span className="text-xs text-muted-foreground">Total</span>
                <span className="text-xs text-muted-foreground">Active</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-lg font-semibold">{stats.torrents.total}</span>
                <span className="text-lg font-semibold">
                  {(stats.torrents.downloading || 0) + (stats.torrents.seeding || 0)}
                </span>
              </div>
            </div>
            
            <div className="flex items-center gap-2 text-xs">
              <Download className="h-3 w-3 text-muted-foreground" />
              <span className="text-muted-foreground">Download</span>
              <span className="ml-auto font-medium">{formatSpeed(stats.speeds?.download || 0)}</span>
            </div>
            
            <div className="flex items-center gap-2 text-xs">
              <Upload className="h-3 w-3 text-muted-foreground" />
              <span className="text-muted-foreground">Upload</span>
              <span className="ml-auto font-medium">{formatSpeed(stats.speeds?.upload || 0)}</span>
            </div>
            
            {stats.torrents.error > 0 && (
              <div className="flex items-center gap-1 text-destructive text-xs pt-2 border-t">
                <AlertCircle className="h-3 w-3" />
                <span>{stats.torrents.error} errors</span>
              </div>
            )}
          </div>
        </CardContent>
      </Card>
    </Link>
  )
}

function GlobalStatsCards({ statsData }: { statsData: Array<{ instance: any, stats: any }> }) {
  const globalStats = useMemo(() => {
    const connected = statsData.filter(({ stats }) => stats?.connected).length
    const totalTorrents = statsData.reduce((sum, { stats }) => 
      sum + (stats?.torrents?.total || 0), 0)
    const activeTorrents = statsData.reduce((sum, { stats }) => 
      sum + ((stats?.torrents?.downloading || 0) + (stats?.torrents?.seeding || 0)), 0)
    const totalDownload = statsData.reduce((sum, { stats }) => 
      sum + (stats?.speeds?.download || 0), 0)
    const totalUpload = statsData.reduce((sum, { stats }) => 
      sum + (stats?.speeds?.upload || 0), 0)
    const totalErrors = statsData.reduce((sum, { stats }) => 
      sum + (stats?.torrents?.error || 0), 0)

    return {
      connected,
      total: statsData.length,
      totalTorrents,
      activeTorrents,
      totalDownload,
      totalUpload,
      totalErrors
    }
  }, [statsData])

  return (
    <>
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Instances</CardTitle>
          <HardDrive className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{globalStats.connected}/{globalStats.total}</div>
          <p className="text-xs text-muted-foreground">
            Connected instances
          </p>
          <Progress 
            value={(globalStats.connected / globalStats.total) * 100} 
            className="mt-2 h-1"
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Total Torrents</CardTitle>
          <Activity className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{globalStats.totalTorrents}</div>
          <p className="text-xs text-muted-foreground">
            {globalStats.activeTorrents} active
          </p>
          {globalStats.totalTorrents > 0 && (
            <Progress 
              value={(globalStats.activeTorrents / globalStats.totalTorrents) * 100} 
              className="mt-2 h-1"
            />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Total Download</CardTitle>
          <Download className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{formatSpeed(globalStats.totalDownload)}</div>
          <p className="text-xs text-muted-foreground">
            All instances combined
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Total Upload</CardTitle>
          <Upload className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{formatSpeed(globalStats.totalUpload)}</div>
          <p className="text-xs text-muted-foreground">
            All instances combined
          </p>
        </CardContent>
      </Card>
    </>
  )
}

function QuickActionsDropdown({ statsData }: { statsData: Array<{ instance: any, stats: any }> }) {
  const connectedInstances = statsData
    .filter(({ stats }) => stats?.connected)
    .map(({ instance }) => instance)

  if (connectedInstances.length === 0) {
    return null
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" className="w-full sm:w-auto">
          <Zap className="h-4 w-4 mr-2" />
          Quick Actions
          <ChevronDown className="h-3 w-3 ml-1" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel>Add Torrent</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {connectedInstances.map(instance => (
          <Link 
            key={instance.id} 
            to="/instances/$instanceId" 
            params={{ instanceId: instance.id.toString() }}
            search={{ modal: 'add-torrent' }}
          >
            <DropdownMenuItem className="cursor-pointer active:bg-accent focus:bg-accent">
              <Plus className="h-4 w-4 mr-2" />
              <span>Add to {instance.name}</span>
            </DropdownMenuItem>
          </Link>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function Dashboard() {
  const { instances, isLoading } = useInstances()
  const allInstances = instances || []
  
  // Use safe hook that always calls the same number of hooks
  const statsData = useAllInstanceStats(allInstances)
  
  if (isLoading) {
    return (
      <div className="container mx-auto p-4 sm:p-6">
        <div className="animate-pulse space-y-4">
          <div className="h-8 bg-muted rounded w-48"></div>
          <div className="h-4 bg-muted rounded w-64"></div>
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
            {[...Array(4)].map((_, i) => (
              <div key={i} className="h-24 bg-muted rounded"></div>
            ))}
          </div>
        </div>
      </div>
    )
  }
  
  return (
    <div className="container mx-auto p-4 sm:p-6">
      {/* Header with Actions */}
      <div className="mb-6">
        <h1 className="text-2xl sm:text-3xl font-bold">Dashboard</h1>
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 mt-2">
          <p className="text-muted-foreground">
            Overview of all your qBittorrent instances
          </p>
          {instances && instances.length > 0 && (
            <div className="flex flex-col sm:flex-row gap-2 w-full sm:w-auto">
              <QuickActionsDropdown statsData={statsData} />
              <Link to="/instances" search={{ modal: 'add-instance' }} className="w-full sm:w-auto">
                <Button variant="outline" size="sm" className="w-full sm:w-auto">
                  <Plus className="h-4 w-4 mr-2" />
                  Add Instance
                </Button>
              </Link>
            </div>
          )}
        </div>
      </div>
      
      {instances && instances.length > 0 ? (
        <div className="space-y-6">
          {/* Global Stats */}
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
            <GlobalStatsCards statsData={statsData} />
          </div>
          
          {/* Instance Cards */}
          {allInstances.length > 0 && (
            <div>
              <h2 className="text-xl font-semibold mb-4">Instances</h2>
              <div className="grid gap-4 sm:grid-cols-1 md:grid-cols-2 lg:grid-cols-3">
                {allInstances.map(instance => (
                  <InstanceCard key={instance.id} instance={instance} />
                ))}
              </div>
            </div>
          )}
        </div>
      ) : (
        <Card className="p-8 sm:p-12 text-center">
          <div className="space-y-4">
            <HardDrive className="h-12 w-12 mx-auto text-muted-foreground" />
            <div>
              <h3 className="text-lg font-semibold">No instances configured</h3>
              <p className="text-muted-foreground">Get started by adding your first qBittorrent instance</p>
            </div>
            <Link to="/instances" search={{ modal: 'add-instance' }}>
              <Button>
                <Plus className="h-4 w-4 mr-2" />
                Add Instance
              </Button>
            </Link>
          </div>
        </Card>
      )}
    </div>
  )
}