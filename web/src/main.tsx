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

setupLaunchQueueConsumer()

// Wait for the active language's resources (lazily loaded for non-English) before
// mounting, so the first paint is already in the user's chosen language.
void initI18n().finally(() => {
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <App />
    </StrictMode>
  )
})
