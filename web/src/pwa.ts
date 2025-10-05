/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

let hasRegistered = false

export function setupPWAAutoUpdate(): void {
  if (hasRegistered) return
  if (!("serviceWorker" in navigator)) return

  hasRegistered = true

  import("virtual:pwa-register").then(({ registerSW }) => {
    const updateSW = registerSW({
      immediate: true,
      onNeedRefresh() {
        updateSW(true).catch((error) => {
          console.error("Failed to apply PWA update", error)
        })
      },
      onRegisterError(error) {
        console.error("Service worker registration failed", error)
      },
    })

    let refreshing = false
    navigator.serviceWorker.addEventListener("controllerchange", () => {
      if (refreshing) return
      refreshing = true
      window.location.reload()
    })
  }).catch((error) => {
    console.error("Failed to load PWA registration module", error)
  })
}
