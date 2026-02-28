/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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
export default defineConfig(() => ({
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
      // Workbox-build uses Rollup + terser when mode=production; that currently breaks builds
      // on some newer Node.js versions. We don't need SW minification, so prefer compatibility.
      mode: "development",
      registerType: "autoUpdate",
      injectRegister: null,
      minify: false,
      devOptions: {
        enabled: false,
      },
      workbox: {
        // Workbox uses rollup+terser in production mode; Node 24 currently triggers an "Unexpected early exit".
        // Use development mode on Node 24+ to keep builds working without changing runtime behavior elsewhere.
        mode: workboxMode,
        globPatterns: ["**/*.{js,css,html,ico,png,svg,webp}"],
        // Keep installation fast/reliable by excluding the massive main app chunk from precache.
        // It is cached at runtime (stale-while-revalidate) below.
        globIgnores: ["assets/index-*.js"],
        disableDevLogs: true,
        // Keep Workbox's precache size guardrail for all other assets.
        maximumFileSizeToCacheInBytes: 3 * 1024 * 1024,
        runtimeCaching: [
          {
            // Cache JS bundles on demand; avoids precache blowups from large app chunks.
            urlPattern: /\/assets\/.*\.js$/i,
            handler: "StaleWhileRevalidate",
            options: {
              cacheName: "js-assets-runtime",
              expiration: {
                maxEntries: 30,
                maxAgeSeconds: 7 * 24 * 60 * 60,
              },
              cacheableResponse: {
                statuses: [0, 200],
              },
            },
          },
        ],
        sourcemap: true,
        // Avoid serving the SPA shell for backend proxy routes and SSO callback paths
        // (also under custom base URLs). /cdn-cgi/ is used by Cloudflare Access for its
        // auth callback (/cdn-cgi/access/authorized); intercepting it breaks the SSO flow.
        navigateFallbackDenylist: [/\/api(?:\/|$)/, /\/proxy(?:\/|$)/, /\/cdn-cgi(?:\/|$)/, /\/\.well-known(?:\/|$)/],
        // Some deployments sit behind Basic Auth; skip assets that tend to 401 (e.g. manifest, source maps)
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
      },
      includeAssets: ["favicon.png", "apple-touch-icon.png"],
      manifest: {
        name: "qui",
        short_name: "qui",
        description: "Alternative WebUI for qBittorrent - manage your torrents with a modern interface",
        theme_color: "#000000", // Will be updated dynamically by PWA theme manager
        background_color: "#000000",
        display: "standalone",
        scope: "./",
        start_url: "./",
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
        protocol_handlers: [
          {
            protocol: "magnet",
            url: "./add?url=%s",
          },
        ],
        file_handlers: [
          {
            action: "./add",
            accept: {
              "application/x-bittorrent": [".torrent"],
            },
          },
        ],
        launch_handler: {
          client_mode: "navigate-existing",
        },
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
}));
