/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { toast } from "sonner"
import { getBaseUrl, withBasePath } from "./lib/base-url"
import i18n from "./i18n"

let hasRegistered = false

export function setupPWAAutoUpdate(): void {
  if (hasRegistered) return
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
        label: i18n.t("pwa.reload"),
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
          title: i18n.t("pwa.updateAvailable.title"),
          description: i18n.t("pwa.updateAvailable.description"),
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
            title: i18n.t("pwa.updated.title"),
            description: i18n.t("pwa.updated.description"),
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
