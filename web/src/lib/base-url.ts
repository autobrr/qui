/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Get the base URL injected by the backend
// Falls back to '/' if not set
export function getBaseUrl(): string {
  // @ts-expect-error - This is injected by the backend
  const baseUrl = window.__QUI_BASE_URL__ || "/"

  // Ensure it ends with /
  return baseUrl.endsWith("/") ? baseUrl : baseUrl + "/"
}

// Get the API base URL
export function getApiBaseUrl(): string {
  const base = getBaseUrl()
  // Remove trailing slash before adding 'api'
  return base.slice(0, -1) + "/api"
}

// Helper to join paths with the base URL
export function withBasePath(path: string): string {
  const base = getBaseUrl()
  // Remove leading slash from path if present
  const cleanPath = path.startsWith("/") ? path.slice(1) : path
  return base + cleanPath
}