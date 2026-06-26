/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

const STORAGE_KEY = "qui.license.entitlement.v1"

// Grace period matches backend offlineGracePeriod (7 days)
export const GRACE_PERIOD_MS = 7 * 24 * 60 * 60 * 1000

export interface LicenseEntitlement {
  lastKnownHasPremiumAccess: boolean
  lastSuccessfulValidationAt: number // Unix timestamp in ms
}

export function getLicenseEntitlement(): LicenseEntitlement | null {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (!stored) return null

    const parsed = JSON.parse(stored) as LicenseEntitlement
    if (
      typeof parsed.lastKnownHasPremiumAccess !== "boolean" ||
      typeof parsed.lastSuccessfulValidationAt !== "number"
    ) {
      return null
    }

    return parsed
  } catch {
    return null
  }
}

export function setLicenseEntitlement(hasPremiumAccess: boolean): void {
  try {
    const entitlement: LicenseEntitlement = {
      lastKnownHasPremiumAccess: hasPremiumAccess,
      lastSuccessfulValidationAt: Date.now(),
    }
    localStorage.setItem(STORAGE_KEY, JSON.stringify(entitlement))
  } catch {
    // ignore localStorage errors
  }
}

export function clearLicenseEntitlement(): void {
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    // ignore localStorage errors
  }
}

export function isWithinGracePeriod(entitlement: LicenseEntitlement | null): boolean {
  if (!entitlement) return false
  const now = Date.now()
  if (entitlement.lastSuccessfulValidationAt > now) return false
  const elapsed = now - entitlement.lastSuccessfulValidationAt
  return elapsed < GRACE_PERIOD_MS
}

export function canSwitchToPremiumTheme(currentApiState: {
  hasPremiumAccess: boolean | undefined
  isError: boolean
  isLoading: boolean
}): boolean {
  const { hasPremiumAccess, isError, isLoading } = currentApiState

  if (isLoading) {
    const stored = getLicenseEntitlement()
    return stored?.lastKnownHasPremiumAccess === true && isWithinGracePeriod(stored)
  }

  if (!isError && hasPremiumAccess !== undefined) {
    return hasPremiumAccess
  }

  if (isError) {
    const stored = getLicenseEntitlement()
    return stored?.lastKnownHasPremiumAccess === true && isWithinGracePeriod(stored)
  }

  return false
}
