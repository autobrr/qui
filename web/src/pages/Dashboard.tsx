import { useInstances } from '@/hooks/useInstances'
import { useInstanceStats } from '@/hooks/useInstanceStats'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import { Button } from '@/components/ui/button'
import { HardDrive, Download, Upload, AlertCircle, Activity, TrendingUp, Plus, Zap } from 'lucide-react'
import { Link } from '@tanstack/react-router'
import { useMemo } from 'react'

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

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`
}

function formatSpeed(bytesPerSecond: number): string {
  if (bytesPerSecond === 0) return '0 B/s'
  return `${formatBytes(bytesPerSecond)}/s`
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
              <Badge variant={instance.isActive ? 'default' : 'destructive'}>
                {instance.isActive ? 'Active' : 'Inactive'}
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
            <Badge variant={instance.isActive && stats.connected ? 'default' : 'destructive'}>
              {instance.isActive && stats.connected ? 'Connected' : 'Disconnected'}
            </Badge>
          </div>
          <CardDescription className="text-xs">{instance.host}:{instance.port}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">Torrents</span>
            <span className="text-sm font-semibold">{stats.torrents.total}</span>
          </div>
          {stats.torrents.error > 0 && (
            <div className="flex items-center gap-1 text-destructive text-xs mt-2 pt-2 border-t">
              <AlertCircle className="h-3 w-3" />
              <span>{stats.torrents.error} errors</span>
            </div>
          )}
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
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
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
          <CardTitle className="text-sm font-medium">Download Speed</CardTitle>
          <Download className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{formatSpeed(globalStats.totalDownload)}</div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Upload Speed</CardTitle>
          <Upload className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{formatSpeed(globalStats.totalUpload)}</div>
        </CardContent>
      </Card>
    </div>
  )
}

function QuickActionsCard({ statsData }: { statsData: Array<{ instance: any, stats: any }> }) {
  const connectedInstances = statsData
    .filter(({ stats }) => stats?.connected)
    .map(({ instance }) => instance)

  if (connectedInstances.length === 0) {
    return null
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Zap className="h-5 w-5" />
          Quick Actions
        </CardTitle>
        <CardDescription>
          Fast access to common tasks
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="flex flex-wrap gap-2">
          {connectedInstances.slice(0, 3).map(instance => (
            <Link 
              key={instance.id} 
              to="/instances/$instanceId" 
              params={{ instanceId: instance.id.toString() }}
              search={{ modal: 'add-torrent' }}
            >
              <Button variant="outline" size="sm" className="h-8">
                <Plus className="h-3 w-3 mr-1" />
                Add to {instance.name}
              </Button>
            </Link>
          ))}
          {connectedInstances.length > 3 && (
            <Badge variant="secondary" className="ml-2">
              +{connectedInstances.length - 3} more
            </Badge>
          )}
        </div>
      </CardContent>
    </Card>
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
      {/* Header */}
      <div className="mb-6">
        <h1 className="text-2xl sm:text-3xl font-bold">Dashboard</h1>
        <p className="text-muted-foreground mt-2">
          Overview of all your qBittorrent instances
        </p>
      </div>
      
      {instances && instances.length > 0 ? (
        <div className="space-y-6">
          {/* Global Stats */}
          <GlobalStatsCards statsData={statsData} />
          
          {/* Quick Actions */}
          <QuickActionsCard statsData={statsData} />
          
          {/* Instance Cards */}
          {allInstances.length > 0 && (
            <div>
              <div className="flex items-center justify-between mb-4">
                <h2 className="text-xl font-semibold">Instances</h2>
                <Link to="/instances" search={{ modal: 'add-instance' }}>
                  <Button variant="outline" size="sm">
                    <Plus className="h-4 w-4 mr-2" />
                    Add Instance
                  </Button>
                </Link>
              </div>
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