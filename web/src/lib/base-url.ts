/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export function getBaseUrl(): string {
  // @ts-expect-error - This is injected by the backend
  const baseUrl = window.__QUI_BASE_URL__ || "/"

  return baseUrl.endsWith("/") ? baseUrl : baseUrl + "/"
}

export function getApiBaseUrl(): string {
  const base = getBaseUrl()
  return base.slice(0, -1) + "/api"
}

export function withBasePath(path: string): string {
  const base = getBaseUrl()
  const cleanPath = path.startsWith("/") ? path.slice(1) : path
  return base + cleanPath
}