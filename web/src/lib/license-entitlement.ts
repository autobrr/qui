/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * License entitlement persistence for graceful handling of transient API errors.
 *
 * Stores the last known premium access state and validation timestamp to:
 * 1. Prevent theme resets on transient license API failures
 * 2. Allow switching to premium themes only when recently validated (within grace period)
 * 3. Keep current premium theme applied during temporary outages
 */

const STORAGE_KEY = "qui.license.entitlement.v1"

// Grace period matches backend's offlineGracePeriod (7 days)
export const GRACE_PERIOD_MS = 7 * 24 * 60 * 60 * 1000

export interface LicenseEntitlement {
  lastKnownHasPremiumAccess: boolean
  lastSuccessfulValidationAt: number // Unix timestamp in ms
}

/**
 * Get the stored license entitlement state
 */
export function getLicenseEntitlement(): LicenseEntitlement | null {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (!stored) return null

    const parsed = JSON.parse(stored) as LicenseEntitlement

    // Validate shape
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

/**
 * Store license entitlement state after a successful API response
 */
export function setLicenseEntitlement(hasPremiumAccess: boolean): void {
  try {
    const entitlement: LicenseEntitlement = {
      lastKnownHasPremiumAccess: hasPremiumAccess,
      lastSuccessfulValidationAt: Date.now(),
    }
    localStorage.setItem(STORAGE_KEY, JSON.stringify(entitlement))
  } catch {
    // Ignore localStorage errors
  }
}

/**
 * Clear stored license entitlement (e.g., on logout or license deletion)
 */
export function clearLicenseEntitlement(): void {
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    // Ignore localStorage errors
  }
}

/**
 * Check if the stored entitlement is within the grace period
 */
export function isWithinGracePeriod(entitlement: LicenseEntitlement | null): boolean {
  if (!entitlement) return false

  const elapsed = Date.now() - entitlement.lastSuccessfulValidationAt
  return elapsed < GRACE_PERIOD_MS
}

/**
 * Determine if premium theme switching should be allowed based on current state.
 *
 * Rules:
 * - If we have a confirmed hasPremiumAccess === true from recent successful validation: allow
 * - If we're in an error/unknown state but have a recent licensed validation within grace: allow
 * - Otherwise: block premium switching
 */
export function canSwitchToPremiumTheme(
  currentApiState: { hasPremiumAccess: boolean | undefined; isError: boolean; isLoading: boolean }
): boolean {
  const { hasPremiumAccess, isError, isLoading } = currentApiState

  // Still loading - check stored entitlement
  if (isLoading) {
    const stored = getLicenseEntitlement()
    return stored?.lastKnownHasPremiumAccess === true && isWithinGracePeriod(stored)
  }

  // Successful response - trust it
  if (!isError && hasPremiumAccess !== undefined) {
    return hasPremiumAccess
  }

  // Error state - check if we have a recent successful licensed validation
  if (isError) {
    const stored = getLicenseEntitlement()
    // Only allow switching if we had premium access AND we're within grace period
    return stored?.lastKnownHasPremiumAccess === true && isWithinGracePeriod(stored)
  }

  // Fallback: no access
  return false
}

/**
 * Determine if the current premium theme should be kept during errors.
 *
 * More lenient than canSwitchToPremiumTheme - we want to avoid disrupting
 * users who are already on a premium theme during transient failures.
 *
 * Rules:
 * - If we have any stored entitlement with premium access within grace: keep current theme
 * - This prevents the jarring UX of theme resets during network blips
 */
export function shouldKeepCurrentPremiumTheme(
  currentApiState: { isError: boolean; isLoading: boolean }
): boolean {
  const { isError, isLoading } = currentApiState

  // Only relevant during error/loading states
  if (!isError && !isLoading) return false

  const stored = getLicenseEntitlement()
  return stored?.lastKnownHasPremiumAccess === true && isWithinGracePeriod(stored)
}
