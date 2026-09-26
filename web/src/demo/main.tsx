/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Entry for the getqui.com demo build. Installs the fake API before the app
// modules evaluate, then boots the unchanged App. Production main.tsx never
// imports this file.

import { createDemoFetch } from "./api"
import { createStore } from "./store"
import { createDemoEventSource } from "./stream"

// lib/api.ts captures API_BASE and router.tsx its basepath at module
// evaluation, so the base URL is set before either module is imported below.
(window as Window & { __QUI_BASE_URL__?: string }).__QUI_BASE_URL__ = import.meta.env.BASE_URL
window.__QUI_VERSION__ = "demo"

// The app's own root redirects to the dashboard, which the demo hides.
if (window.location.pathname === import.meta.env.BASE_URL) {
  window.history.replaceState(null, "", `${import.meta.env.BASE_URL}instances/1`)
}

const store = createStore()
const apiBase = `${import.meta.env.BASE_URL.replace(/\/$/, "")}/api`
window.fetch = createDemoFetch(store, apiBase, window.fetch.bind(window))
window.EventSource = createDemoEventSource(store) as unknown as typeof EventSource


void import("../main")
