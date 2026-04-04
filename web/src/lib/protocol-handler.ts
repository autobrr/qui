/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { withBasePath } from "./base-url"

const DISMISSED_KEY = "qui-protocol-handler-dismissed"

function isStandaloneDisplayMode(): boolean {
  return window.matchMedia("(display-mode: standalone)").matches ||
    (navigator as Navigator & { standalone?: boolean }).standalone === true
}

function isFirefox(): boolean {
  return /firefox/i.test(navigator.userAgent)
}

function isChromium(): boolean {
  const ua = navigator.userAgent
  return /(chrome|chromium|crios)/i.test(ua) && !/(edg|opr|opera)/i.test(ua)
}

export type MagnetHandlerGuidanceVariant = "standalone" | "firefox" | "chromium" | "default"

/**
 * Check if the browser supports registerProtocolHandler and we're in a secure context.
 * Secure contexts include HTTPS and localhost (even over HTTP).
 * This complements PWA manifest protocol_handlers for browsers that don't support it.
 */
export function canRegisterProtocolHandler(): boolean {
  return typeof navigator?.registerProtocolHandler === "function"
    && window.isSecureContext
}

export function getMagnetHandlerRegistrationGuidanceVariant(): MagnetHandlerGuidanceVariant {
  if (isStandaloneDisplayMode()) {
    return "standalone"
  }

  if (isFirefox()) {
    return "firefox"
  }

  if (isChromium()) {
    return "chromium"
  }

  return "default"
}

/**
 * Register qui as the handler for magnet: links.
 * Returns true if registration was requested, false if it failed.
 * The browser may prompt the user for confirmation.
 */
export function registerMagnetHandler(): boolean {
  try {
    const handlerUrl = `${window.location.origin}${withBasePath("/add")}?url=%s`
    // Some browsers support a third title argument but TypeScript only knows about the first two.
    // eslint-disable-next-line @typescript-eslint/ban-ts-comment
    // @ts-expect-error registerProtocolHandler accepts an optional title argument in practice
    navigator.registerProtocolHandler("magnet", handlerUrl, "qui")
    return true
  } catch (error) {
    console.error("Failed to register magnet handler:", error)
    return false
  }
}

/**
 * Check if the user has dismissed the protocol handler banner.
 */
export function isProtocolHandlerBannerDismissed(): boolean {
  try {
    return localStorage.getItem(DISMISSED_KEY) === "true"
  } catch {
    return false
  }
}

/**
 * Dismiss the protocol handler banner permanently.
 */
export function dismissProtocolHandlerBanner(): void {
  try {
    localStorage.setItem(DISMISSED_KEY, "true")
  } catch (error) {
    console.warn("Failed to persist banner dismissal:", error)
  }
}
