/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getBaseUrl, withBasePath } from "./lib/base-url"

let hasRegistered = false

export function setupPWAAutoUpdate(): void {
  if (hasRegistered) return
  if (!("serviceWorker" in navigator)) return

  hasRegistered = true

  const scope = getBaseUrl()
  const swUrl = withBasePath("sw.js")
  let refreshing = false

  const reload = () => {
    if (refreshing) return
    refreshing = true
    window.location.reload()
  }

  import("workbox-window")
    .then(({ Workbox }) => {
      const wb = new Workbox(swUrl, { scope })

      wb.addEventListener("waiting", () => {
        try {
          wb.messageSkipWaiting()
        } catch (error) {
          console.error("Failed to trigger service worker update", error)
        }
      })

      wb.addEventListener("activated", (event) => {
        if (event.isUpdate || event.isExternal) {
          reload()
        }
      })

      wb.addEventListener("controlling", (event) => {
        if (event.isUpdate) {
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

  navigator.serviceWorker.addEventListener("controllerchange", reload)
}
