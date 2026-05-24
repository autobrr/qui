/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import react from "@vitejs/plugin-react"
import path from "node:path"
import { fileURLToPath } from "node:url"
import { defineConfig } from "vitest/config"

// Standalone config (does not import vite.config.ts) so the PWA / node-polyfills /
// tailwind plugins don't run during test startup. We only need the @/* alias and
// the React plugin for transforming JSX/TSX.
const __dirname = path.dirname(fileURLToPath(import.meta.url))

export default defineConfig({
  plugins: [
    react({
      jsxRuntime: "automatic",
    }),
  ],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  test: {
    environment: "jsdom",
    globals: false,
    include: ["src/**/*.test.{ts,tsx}"],
  },
})
