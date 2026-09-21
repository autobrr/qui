/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { toast } from "sonner"
import i18n from "./i18n"
import { getBaseUrl, withBasePath } from "./lib/base-url"
import { isDemo } from "./lib/demo"

let hasRegistered = false

export function setupPWAAutoUpdate(): void {
  if (hasRegistered || isDemo) return
  if (!("serviceWorker" in navigator)) return

  hasRegistered = true

  const scope = getBaseUrl()
  const swUrl = withBasePath("sw.js")
  let refreshing = false
  let shouldReloadAfterActivation = false
  let updateToastId: string | number | undefined

  const reload = () => {
    if (refreshing) return
    refreshing = true
    window.location.reload()
  }

  const dismissUpdateToast = () => {
    if (updateToastId === undefined) return
    toast.dismiss(updateToastId)
    updateToastId = undefined
  }

  const showUpdateToast = ({
    title,
    description,
    onConfirm,
  }: {
    title: string
    description: string
    onConfirm: () => void
  }) => {
    if (updateToastId !== undefined) return

    updateToastId = toast(title, {
      description,
      duration: Number.POSITIVE_INFINITY,
      action: {
        label: i18n.t("pwaUpdate.reload", { ns: "common" }),
        onClick: () => {
          dismissUpdateToast()
          onConfirm()
        },
      },
      onDismiss: () => {
        updateToastId = undefined
      },
    })
  }

  import("workbox-window")
    .then(({ Workbox }) => {
      const wb = new Workbox(swUrl, { scope })

      const promptForUpdate = () => {
        showUpdateToast({
          title: i18n.t("updateBanner.updateAvailable", { ns: "common" }),
          description: i18n.t("pwaUpdate.waitingDescription", { ns: "common" }),
          onConfirm: () => {
            shouldReloadAfterActivation = true

            try {
              wb.messageSkipWaiting()
            } catch (error) {
              console.error("Failed to trigger service worker update", error)
              shouldReloadAfterActivation = false
              reload()
            }
          },
        })
      }

      wb.addEventListener("waiting", () => {
        promptForUpdate()
      })

      wb.addEventListener("activated", (event) => {
        if (shouldReloadAfterActivation) {
          reload()
          return
        }

        if (event.isUpdate || event.isExternal) {
          showUpdateToast({
            title: i18n.t("pwaUpdate.activatedTitle", { ns: "common" }),
            description: i18n.t("pwaUpdate.activatedDescription", { ns: "common" }),
            onConfirm: () => {
              reload()
            },
          })
        }
      })

      wb.addEventListener("controlling", (event) => {
        if (shouldReloadAfterActivation && event.isUpdate) {
          dismissUpdateToast()
          reload()
        }
      })

      wb.register({ immediate: true }).catch((error) => {
        console.error("Service worker registration failed", error)
      })
    })
    .catch((error) => {
      console.error("Failed to load Workbox for PWA registration", error)
    })

  navigator.serviceWorker.addEventListener("controllerchange", () => {
    if (!shouldReloadAfterActivation) return
    dismissUpdateToast()
    reload()
  })
}
