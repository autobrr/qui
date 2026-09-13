/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import App from "./App.tsx"
import { setupLaunchQueueConsumer } from "@/lib/launch-queue"
import { initI18n } from "./i18n"
import "./index.css"
import "./spreadsheet-chrome.css"

setupLaunchQueueConsumer()

// An upgrade replaces every hashed chunk, so a tab opened before it fails its
// first dynamic import. Reload to pick up the new asset set.
// One reload per 10 s: a chunk that is still missing after a reload surfaces
// as a route error instead of a reload loop.
window.addEventListener("vite:preloadError", (event) => {
  const last = Number(sessionStorage.getItem("preloadErrorReload") ?? 0)
  if (Date.now() - last < 10_000) return
  sessionStorage.setItem("preloadErrorReload", String(Date.now()))
  event.preventDefault()
  window.location.reload()
})

// Wait for the active language's resources (lazily loaded for non-English) before
// mounting, so the first paint is already in the user's chosen language.
void initI18n().finally(() => {
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <App />
    </StrictMode>
  )
})
