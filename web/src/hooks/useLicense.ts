/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import { getLicenseErrorMessage } from "@/lib/license-errors.ts"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { t } from "i18next"
import { toast } from "sonner"

// Hook to check premium access status
export const usePremiumAccess = () => {
  return useQuery({
    queryKey: ["licenses"],
    queryFn: () => api.getLicensedThemes(),
    staleTime: 30 * 1000, // 30 seconds
    refetchInterval: 30 * 1000, // Poll every 30 seconds
    refetchOnWindowFocus: false,
  })
}

// Hook to activate a license
export const useActivateLicense = () => {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (licenseKey: string) => api.activateLicense(licenseKey),
    onSuccess: (data) => {
      if (data.valid) {
        const message = t("hooks.useLicense.premiumAccessActivated")
        toast.success(message)
        // Invalidate license queries to refresh the UI
        queryClient.invalidateQueries({ queryKey: ["licenses"] })
      }
    },
    onError: (error: Error) => {
      toast.error(getLicenseErrorMessage(error))
    },
  })
}

// Hook to validate a license
export const useValidateLicense = () => {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (licenseKey: string) => api.validateLicense(licenseKey),
    onSuccess: (data) => {
      if (data.valid) {
        const message = data.productName === "premium-access"? t("hooks.useLicense.premiumAccessActivated"): t("hooks.useLicense.licenseActivated")
        toast.success(message)
        // Invalidate license queries to refresh the UI
        queryClient.invalidateQueries({ queryKey: ["licenses"] })
      }
    },
    onError: (error: Error) => {
      toast.error(getLicenseErrorMessage(error))
    },
  })
}

// Hook to delete a license
export const useDeleteLicense = () => {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (licenseKey: string) => api.deleteLicense(licenseKey),
    onSuccess: () => {
      toast.success(t("hooks.useLicense.licenseReleased"))
      // Invalidate license queries to refresh the UI
      queryClient.invalidateQueries({ queryKey: ["licenses"] })
    },
    onError: (error: Error) => {
      toast.error(error.message || t("hooks.useLicense.failedToReleaseLicense"))
    },
  })
}

// Hook to refresh all licenses
export const useRefreshLicenses = () => {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.refreshLicenses(),
    onSuccess: () => {
      toast.success(t("hooks.useLicense.licensesRefreshed"))
      // Invalidate license queries to refresh the UI
      queryClient.invalidateQueries({ queryKey: ["licenses"] })
    },
    onError: (error: Error) => {
      toast.error(error.message || t("hooks.useLicense.failedToRefreshLicenses"))
    },
  })
}

// Helper hook to check if user has premium access
export const useHasPremiumAccess = () => {
  const { data, isLoading } = usePremiumAccess()

  return {
    hasPremiumAccess: data?.hasPremiumAccess ?? false,
    isLoading,
  }
}

// Hook to get license details for management
export const useLicenseDetails = () => {
  return useQuery({
    queryKey: ["licenses", "all"],
    queryFn: () => api.getAllLicenses(),
    staleTime: 30 * 60 * 1000, // 30 minutes
    refetchOnWindowFocus: false,
  })
}