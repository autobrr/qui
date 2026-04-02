interface ResolveTrackerHealthSupportOptions {
  isUnifiedView: boolean
  capabilitySupport?: boolean
  responseSupport?: boolean
}

export function resolveTrackerHealthSupport({
  isUnifiedView,
  capabilitySupport = false,
  responseSupport = false,
}: ResolveTrackerHealthSupportOptions): boolean {
  if (isUnifiedView) {
    return responseSupport
  }

  return capabilitySupport
}
