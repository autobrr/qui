export interface BlocklistInstanceOption {
  id: number
}

export interface DirScanRunStatsLike {
  filesFound: number
  filesSkipped: number
  matchesFound: number
  torrentsAdded: number
}

export function getNextBlocklistInstanceId(
  instances: readonly BlocklistInstanceOption[],
  instanceId: number | null
): number | null {
  if (instances.length === 0) {
    return null
  }

  if (instanceId !== null && instances.some((instance) => instance.id === instanceId)) {
    return instanceId
  }

  return instances[0].id
}

export function getRunDiscoveredFiles(run: DirScanRunStatsLike): number {
  return run.filesFound + run.filesSkipped
}

export function shouldShowRunFileDetails(run: DirScanRunStatsLike): boolean {
  return getRunDiscoveredFiles(run) > run.filesFound
}

export function hasDirScanStatusStats(run: DirScanRunStatsLike): boolean {
  return run.filesFound > 0 || run.filesSkipped > 0 || run.matchesFound > 0 || run.torrentsAdded > 0
}
