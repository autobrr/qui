/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import { getLicenseErrorMessage } from "@/lib/license-errors.ts"
import { clearLicenseEntitlement, setLicenseEntitlement } from "@/lib/license-entitlement"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useEffect } from "react"
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

  useEffect(() => {
    if (query.data) {
      setLicenseEntitlement(query.data.hasPremiumAccess)
    }
  }, [query.data])

  return query
}

// Hook to activate a license
export const useActivateLicense = () => {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (licenseKey: string) => api.activateLicense(licenseKey),
    onSuccess: (data) => {
      if (data.valid) {
        const message = "Premium access activated! Thank you!"
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
        const message = data.productName === "premium-access"? "Premium access activated! Thank you!": "License activated successfully!"
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
      toast.success("License removed from this machine")
      clearLicenseEntitlement()
      // Invalidate license queries to refresh the UI
      queryClient.invalidateQueries({ queryKey: ["licenses"] })
    },
    onError: (error: Error) => {
      toast.error(getLicenseErrorMessage(error))
    },
  })
}

// Hook to refresh all licenses
export const useRefreshLicenses = () => {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.refreshLicenses(),
    onSuccess: () => {
      toast.success("All licenses refreshed successfully")
      // Invalidate license queries to refresh the UI
      queryClient.invalidateQueries({ queryKey: ["licenses"] })
    },
    onError: (error: Error) => {
      toast.error(error.message || "Failed to refresh licenses")
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
