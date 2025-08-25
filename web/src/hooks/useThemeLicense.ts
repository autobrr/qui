/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { toast } from "sonner"

// Hook to get all licensed themes
export const useLicensedThemes = () => {
  return useQuery({
    queryKey: ["theme-licenses"],
    queryFn: () => api.getLicensedThemes(),
    staleTime: 30 * 60 * 1000, // 30 minutes
    refetchOnWindowFocus: false,
  })
}


// Hook to validate a theme license
export const useValidateThemeLicense = () => {
  const queryClient = useQueryClient()
  
  return useMutation({
    mutationFn: (licenseKey: string) => api.validateThemeLicense(licenseKey),
    onSuccess: (data) => {
      if (data.valid) {
        const message = data.themeName === "premium-access"? "Premium access activated! Thank you!": "License activated successfully!"
        toast.success(message)
        // Invalidate theme license queries to refresh the UI
        queryClient.invalidateQueries({ queryKey: ["theme-licenses"] })
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || "Failed to validate license")
    },
  })
}

// Hook to delete a theme license
export const useDeleteThemeLicense = () => {
  const queryClient = useQueryClient()
  
  return useMutation({
    mutationFn: (licenseKey: string) => api.deleteThemeLicense(licenseKey),
    onSuccess: () => {
      toast.success("License released successfully")
      // Invalidate theme license queries to refresh the UI
      queryClient.invalidateQueries({ queryKey: ["theme-licenses"] })
      queryClient.invalidateQueries({ queryKey: ["theme-license"] })
    },
    onError: (error: Error) => {
      toast.error(error.message || "Failed to release license")
    },
  })
}

// Hook to refresh all theme licenses
export const useRefreshThemeLicenses = () => {
  const queryClient = useQueryClient()
  
  return useMutation({
    mutationFn: () => api.refreshThemeLicenses(),
    onSuccess: () => {
      toast.success("All licenses refreshed successfully")
      // Invalidate theme license queries to refresh the UI
      queryClient.invalidateQueries({ queryKey: ["theme-licenses"] })
      queryClient.invalidateQueries({ queryKey: ["theme-license"] })
    },
    onError: (error: Error) => {
      toast.error(error.message || "Failed to refresh licenses")
    },
  })
}

// Helper hook to check if user has premium access
export const useHasPremiumAccess = () => {
  const { data, isLoading } = useLicensedThemes()
  
  return {
    hasPremiumAccess: data?.hasPremiumAccess ?? false,
    isLoading,
  }
}

// Hook to get all licenses
export const useAllLicenses = () => {
  return useQuery({
    queryKey: ["theme-licenses", "all"],
    queryFn: () => api.getAllLicenses(),
    staleTime: 30 * 60 * 1000, // 30 minutes
    refetchOnWindowFocus: false,
  })
}