/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import i18n from "i18next"
import { initReactI18next } from "react-i18next"

const localeFiles = import.meta.glob("./locales/**/*.json", { eager: true, import: "default" })

const resources: Record<string, Record<string, unknown>> = {}

for (const [path, module] of Object.entries(localeFiles)) {
  const match = path.match(/\.\/locales\/([^/]+)\/([^/]+)\.json$/)
  if (match) {
    const [, lng, ns] = match
    if (!resources[lng]) resources[lng] = {}
    resources[lng][ns] = module
  }
}

export const supportedLanguages = ["en", "zh-CN", "fr"] as const
export type AppLanguage = (typeof supportedLanguages)[number]
const LANGUAGE_STORAGE_KEY = "qui.language"

export const languageNames: Record<AppLanguage, string> = {
  en: "English",
  "zh-CN": "\u7B80\u4F53\u4E2D\u6587",
  fr: "Français",
}

function isAppLanguage(value: string | null): value is AppLanguage {
  return value !== null && supportedLanguages.includes(value as AppLanguage)
}

function detectBrowserLanguage(): AppLanguage | null {
  const browserLanguages = navigator.languages ?? [navigator.language]
  for (const lang of browserLanguages) {
    // Exact match (e.g. "fr" → "fr", "zh-CN" → "zh-CN")
    if (isAppLanguage(lang)) return lang
    // Prefix match (e.g. "fr-FR" → "fr", "zh-Hans" → skip if no match)
    const prefix = lang.split("-")[0]
    const match = supportedLanguages.find((s) => s === prefix || s.startsWith(`${prefix}-`))
    if (match) return match
  }
  return null
}

function getStoredLanguage(): AppLanguage | null {
  try {
    const storedLanguage = localStorage.getItem(LANGUAGE_STORAGE_KEY)
    return isAppLanguage(storedLanguage) ? storedLanguage : null
  } catch (error) {
    console.error("Failed to read language preference from localStorage:", error)
    return null
  }
}

function persistLanguage(lng: AppLanguage) {
  try {
    localStorage.setItem(LANGUAGE_STORAGE_KEY, lng)
  } catch (error) {
    console.error("Failed to save language preference to localStorage:", error)
  }
}

export function changeLanguage(lng: AppLanguage) {
  persistLanguage(lng)
  return i18n.changeLanguage(lng)
}

export const namespaces = [
  "common",
  "auth",
  "settings",
  "torrents",
  "dashboard",
  "crossseed",
  "rss",
  "search",
  "instances",
  "automations",
] as const

i18n.use(initReactI18next).init({
  resources,
  lng: getStoredLanguage() ?? detectBrowserLanguage() ?? "en",
  fallbackLng: "en",
  defaultNS: "common",
  ns: [...namespaces],
  interpolation: {
    escapeValue: false, // React already escapes
  },
})

export default i18n
