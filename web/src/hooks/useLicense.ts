/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import { LICENSE_PORTAL_URL, SUPPORTER_PRODUCT_NAME } from "@/lib/dodo-constants"
import { getLicenseErrorKey } from "@/lib/license-errors.ts"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

export const usePremiumAccess = () => {
  const query = useQuery({
    queryKey: ["licenses"],
    queryFn: () => api.getLicensedThemes(),
    staleTime: 60 * 60 * 1000,
    refetchInterval: 60 * 60 * 1000,
    refetchOnWindowFocus: false,
    refetchOnReconnect: true,
    retry: 2,
  })

  return query
}

export const useActivateLicense = () => {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (licenseKey: string) => api.activateLicense(licenseKey),
    onSuccess: (data) => {
      if (data.valid) {
        toast.success(t(data.productName === SUPPORTER_PRODUCT_NAME ? "themes.license.toasts.activationSuccessSupporter" : "themes.license.toasts.activationSuccessPremium"))
        queryClient.invalidateQueries({ queryKey: ["licenses"] })
        queryClient.invalidateQueries({ queryKey: ["builtin-themes"] })
      }
    },
    onError: (error: Error) => {
      const key = getLicenseErrorKey(error) ?? "themes.license.errors.generic"
      if (key === "themes.license.errors.activationLimit") {
        toast.error(t("themes.license.errors.limitReached"), {
          duration: 10_000,
          action: {
            label: t("themes.license.actions.openLicensePortal"),
            onClick: () => window.open(LICENSE_PORTAL_URL, "_blank", "noopener,noreferrer"),
          },
        })
        return
      }
      toast.error(t(key))
    },
  })
}

export const useDeleteLicense = () => {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (licenseKey: string) => api.deleteLicense(licenseKey),
    onSuccess: () => {
      toast.success(t("themes.license.toasts.removedFromMachine"))
      queryClient.invalidateQueries({ queryKey: ["licenses"] })
      queryClient.invalidateQueries({ queryKey: ["builtin-themes"] })
    },
    onError: () => {
      toast.error(t("themes.license.errors.deleteFailed"))
    },
  })
}

export const useHasPremiumAccess = () => {
  const { data, isLoading, isError } = usePremiumAccess()

  return {
    hasPremiumAccess: data?.hasPremiumAccess ?? false,
    isSupporter: data?.isSupporter ?? false,
    setupAt: data?.setupAt,
    isLoading,
    isError,
  }
}

export const useLicenseDetails = () => {
  return useQuery({
    queryKey: ["licenses", "all"],
    queryFn: () => api.getAllLicenses(),
    staleTime: 30 * 60 * 1000,
    refetchOnWindowFocus: false,
  })
}
