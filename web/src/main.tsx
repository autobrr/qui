/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import App from "./App.tsx"
import { keepDocumentAtTop } from "@/lib/document-scroll"
import { setupLaunchQueueConsumer } from "@/lib/launch-queue"
import { initializePWANativeTheme } from "@/utils/pwaNativeTheme"
import { initializeTheme } from "@/utils/theme"
import { initI18n } from "./i18n"
import { setupPWAAutoUpdate } from "./pwa"
import "./index.css"
import "./spreadsheet-chrome.css"

setupLaunchQueueConsumer()
keepDocumentAtTop()

void initI18n().finally(() => {
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <App />
    </StrictMode>
  )
  initializeTheme().catch(console.error)
  initializePWANativeTheme()
  if (import.meta.env.PROD) {
    setupPWAAutoUpdate()
  }
})
