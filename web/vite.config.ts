/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"
import { VitePWA } from "vite-plugin-pwa"
import { nodePolyfills } from "vite-plugin-node-polyfills"
import { TanStackRouterVite } from "@tanstack/router-plugin/vite"
import path from "node:path"
import { fileURLToPath } from "node:url"

const __dirname = path.dirname(fileURLToPath(import.meta.url))

// https://vite.dev/config/
export default defineConfig(() => ({
  plugins: [
    TanStackRouterVite(),
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
        enabled: false,
      },
      workbox: {
        globPatterns: ["**/*.{js,css,html,ico,png,svg,webp}"],
        maximumFileSizeToCacheInBytes: 3 * 1024 * 1024, // Allow larger bundles to be precached
        sourcemap: true,
        // Avoid serving the SPA shell for backend proxy routes (also under custom base URLs)
        navigateFallbackDenylist: [/^\/api/, /\/proxy(?:\/|$)/],
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
        scope: "/",
        start_url: "/",
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
    minify: false,
    sourcemap: true,
  },
}));
