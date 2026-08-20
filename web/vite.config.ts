/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
        // ~140 flag SVGs plus a 420 kB flag stylesheet, for a tab most users never
        // open; content-hashed, so the HTTP cache still covers repeat views.
        globIgnores: ["assets/*.svg", "assets/flag-icons-*.css"],
        disableDevLogs: true,
        // VitePWA defaults to 2 MiB; our main bundle can exceed that, which breaks CI builds.
        maximumFileSizeToCacheInBytes: 3 * 1024 * 1024,
        sourcemap: false,
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
  experimental: {
    // The Go handler rewrites the absolute /assets/ paths in index.html for
    // base-URL deployments, but never rewrites CSS content, so CSS url()
    // assets (large flag icons) 404 under a subpath. Emit CSS urls relative
    // to the stylesheet; they resolve correctly for any base. HTML keeps the
    // absolute paths the handler rewrite depends on.
    renderBuiltUrl: (_filename, { hostType }) => hostType === "css" ? { relative: true } : undefined,
  },
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
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            {
              name: "react-vendor",
              test: /node_modules[\\/](react|react-dom)([\\/]|$)/,
              priority: 30,
            },
            {
              name: "tanstack",
              test: /node_modules[\\/]@tanstack[\\/]/,
              priority: 20,
            },
            {
              name: "ui-vendor",
              test: /node_modules[\\/](@radix-ui|lucide-react)([\\/]|$)/,
              priority: 10,
            },
          ],
        },
      },
    },
    chunkSizeWarningLimit: 750,
    sourcemap: false,
  },
}));
