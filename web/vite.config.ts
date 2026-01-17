/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/// <reference lib="WebWorker" />

import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import path from "node:path"
import { fileURLToPath } from "node:url"
import { defineConfig } from "vite"
import { nodePolyfills } from "vite-plugin-node-polyfills"
import { VitePWA } from "vite-plugin-pwa"

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const nodeMajor = Number(process.versions.node.split(".")[0] ?? 0)
const workboxMode = nodeMajor >= 24 ? "development" : "production"

// https://vite.dev/config/
export default defineConfig(() => {
  const enableDevPWA = process.env.VITE_PWA_DEV === "true"

  const imageRuntimeCaching = {
    urlPattern: ({ request }: { request: Request }) => request.destination === "image",
    handler: "CacheFirst" as const,
    options: {
      cacheName: "image-cache",
      expiration: {
        maxEntries: 160,
        maxAgeSeconds: 60 * 60 * 24 * 14,
      },
      cacheableResponse: {
        statuses: [0, 200],
      },
    },
  }

  const fontRuntimeCaching = {
    urlPattern: ({ request }: { request: Request }) => request.destination === "font",
    handler: "CacheFirst" as const,
    options: {
      cacheName: "font-cache",
      expiration: {
        maxEntries: 30,
        maxAgeSeconds: 60 * 60 * 24 * 30,
      },
      cacheableResponse: {
        statuses: [0, 200],
      },
    },
  }

  return {
  plugins: [
    react({
      // React 19 requires the new JSX transform
      jsxRuntime: "automatic",
    }),
    tailwindcss(),
    nodePolyfills({
      // Enable polyfills for Node.js built-in modules
      // Required for parse-torrent library to work in the browser
      include: ["path", "buffer", "stream"],
    }),
    VitePWA({
      registerType: "autoUpdate",
      injectRegister: null,
      minify: false,
      devOptions: {
        enabled: enableDevPWA,
      },
      workbox: {
        // Workbox uses rollup+terser in production mode; Node 24 currently triggers an "Unexpected early exit".
        // Use development mode on Node 24+ to keep builds working without changing runtime behavior elsewhere.
        mode: workboxMode,
        globPatterns: ["**/*.{js,css,html,ico,png,svg,webp}"],
        sourcemap: true,
        navigateFallback: "/index.html",
        navigateFallbackDenylist: [/\/api(?:\/|$)/, /\/proxy(?:\/|$)/],
        manifestTransforms: [
          async (entries) => {
            const manifest = entries.filter((entry) => {
              const url = entry.url || ""
              if (url.endsWith("manifest.webmanifest")) {
                return false
              }
              if (url.endsWith(".map")) {
                return false
              }
              return true
            })
            return { manifest, warnings: [] }
          },
        ],
        runtimeCaching: [imageRuntimeCaching, fontRuntimeCaching],
      },
      includeAssets: ["favicon.png", "apple-touch-icon.png"],
      manifest: {
        name: "qui",
        short_name: "qui",
        description: "Alternative WebUI for qBittorrent - manage your torrents with a modern interface",
        theme_color: "#000000", 
        background_color: "#000000",
        display: "standalone",
        scope: "/",
        start_url: "/",
        display_override: ["standalone", "browser"],
        categories: ["utilities", "productivity"],
        icons: [
          {
            src: "pwa-192x192.png",
            sizes: "192x192",
            type: "image/png",
            purpose: "any",
          },
          {
            src: "pwa-512x512.png",
            sizes: "512x512",
            type: "image/png",
            purpose: "any",
          },
          {
            src: "pwa-512x512.png",
            sizes: "512x512",
            type: "image/png",
            purpose: "maskable",
          },
        ],
      },
    }),
  ],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/api": {
        target: "http://localhost:7476",
        changeOrigin: true,
      },
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          "react-vendor": ["react", "react-dom", "react-hook-form"],
          "tanstack": ["@tanstack/react-router", "@tanstack/react-query", "@tanstack/react-table", "@tanstack/react-virtual"],
          "ui-vendor": ["@radix-ui/react-dialog", "@radix-ui/react-dropdown-menu", "lucide-react"],
        },
      },
    },
    chunkSizeWarningLimit: 750,
    sourcemap: true,
  },
  }
});
