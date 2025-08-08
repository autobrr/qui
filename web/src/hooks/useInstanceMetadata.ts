import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'

interface InstanceMetadata {
  categories: Record<string, { name: string; savePath: string }>
  tags: string[]
  counts: {
    status: Record<string, number>
    categories: Record<string, number>
    tags: Record<string, number>
    trackers: Record<string, number>
    total: number
  }
}

/**
 * Shared hook for fetching instance metadata (categories, tags, counts)
 * This prevents duplicate API calls when multiple components need the same data
 */
export function useInstanceMetadata(instanceId: number) {
  return useQuery<InstanceMetadata>({
    queryKey: ['instance-metadata', instanceId],
    queryFn: async () => {
      // Fetch all metadata in parallel for efficiency
      const [categories, tags, counts] = await Promise.all([
        api.getCategories(instanceId),
        api.getTags(instanceId),
        api.getTorrentCounts(instanceId)
      ])
      
      return { categories, tags, counts }
    },
    staleTime: 60000, // 1 minute - metadata doesn't change often
    gcTime: 300000, // Keep in cache for 5 minutes (was cacheTime in v4, now gcTime in v5)
    refetchInterval: 30000, // Refetch every 30 seconds
    refetchIntervalInBackground: false, // Don't refetch when tab is not active
  })
}