/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import { getLicenseErrorMessage } from "@/lib/license-errors.ts"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

// Hook to check premium access status
export const usePremiumAccess = () => {
  const query = useQuery({
    queryKey: ["licenses"],
    queryFn: () => api.getLicensedThemes(),
    staleTime: 60 * 60 * 1000, // 1 hour
    refetchInterval: 60 * 60 * 1000, // Poll every 1 hour
    refetchOnWindowFocus: false,
    refetchOnReconnect: true,
    retry: 2,
  })

  return query
}

// Hook to activate a license
export const useActivateLicense = () => {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (licenseKey: string) => api.activateLicense(licenseKey),
    onSuccess: (data) => {
      if (data.valid) {
        const message = t("themes.license.toasts.activationSuccessPremium")
        toast.success(message)
        // Invalidate license queries to refresh the UI
        queryClient.invalidateQueries({ queryKey: ["licenses"] })
        queryClient.invalidateQueries({ queryKey: ["builtin-themes"] })
      }
    },
    onError: (error: Error) => {
      toast.error(getLicenseErrorMessage(error))
    },
  })
}

// Hook to validate a license
export const useValidateLicense = () => {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (licenseKey: string) => api.validateLicense(licenseKey),
    onSuccess: (data) => {
      if (data.valid) {
        const message = data.productName === "premium-access"
          ? t("themes.license.toasts.activationSuccessPremium")
          : t("themes.license.toasts.activationSuccess")
        toast.success(message)
        // Invalidate license queries to refresh the UI
        queryClient.invalidateQueries({ queryKey: ["licenses"] })
        queryClient.invalidateQueries({ queryKey: ["builtin-themes"] })
      }
    },
    onError: (error: Error) => {
      toast.error(getLicenseErrorMessage(error))
    },
  })
}

// Hook to delete a license
export const useDeleteLicense = () => {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (licenseKey: string) => api.deleteLicense(licenseKey),
    onSuccess: () => {
      toast.success(t("themes.license.toasts.removedFromMachine"))
      // Invalidate license queries to refresh the UI
      queryClient.invalidateQueries({ queryKey: ["licenses"] })
      queryClient.invalidateQueries({ queryKey: ["builtin-themes"] })
    },
    onError: (error: Error) => {
      toast.error(getLicenseErrorMessage(error))
    },
  })
}

// Hook to refresh all licenses
export const useRefreshLicenses = () => {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.refreshLicenses(),
    onSuccess: () => {
      toast.success(t("themes.license.toasts.refreshedAll"))
      // Invalidate license queries to refresh the UI
      queryClient.invalidateQueries({ queryKey: ["licenses"] })
      queryClient.invalidateQueries({ queryKey: ["builtin-themes"] })
    },
    onError: (error: Error) => {
      toast.error(error.message || t("themes.license.toasts.refreshFailed"))
    },
  })
}

// Helper hook to check if user has premium access
export const useHasPremiumAccess = () => {
  const { data, isLoading, isError } = usePremiumAccess()

  return {
    hasPremiumAccess: data?.hasPremiumAccess ?? false,
    isLoading,
    isError,
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
