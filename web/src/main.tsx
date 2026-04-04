/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import App from "./App.tsx"
import { setupLaunchQueueConsumer } from "@/lib/launch-queue"
import { i18nReady } from "@/i18n"
import "./index.css"

setupLaunchQueueConsumer()

const root = createRoot(document.getElementById("root")!)

function renderApp() {
  root.render(
    <StrictMode>
      <App />
    </StrictMode>
  )
}

void i18nReady
  .catch((error) => {
    console.error("Failed to initialize i18n during app bootstrap", error)
  })
  .finally(renderApp)
