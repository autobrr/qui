/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import i18n, { type ResourceKey, type ResourceLanguage } from "i18next"
import { initReactI18next } from "react-i18next"

// English is the fallback and is always needed, so bundle it eagerly into the main
// chunk. Every other language is split into its own lazily-loaded chunk so the main
// bundle stays small and adding languages does not grow it (keeps the PWA precache
// happy). See loadLanguageResources / changeLanguage below.
const enModules = import.meta.glob("./locales/en/*.json", { eager: true, import: "default" }) as Record<string, ResourceKey>
const lazyLoaders = import.meta.glob("./locales/**/*.json", { import: "default" }) as Record<string, () => Promise<ResourceKey>>

function parseLocalePath(path: string): { lng: string, ns: string } | null {
  const match = path.match(/\.\/locales\/([^/]+)\/([^/]+)\.json$/)
  return match ? { lng: match[1], ns: match[2] } : null
}

const enResources: ResourceLanguage = {}
for (const [path, module] of Object.entries(enModules)) {
  const parsed = parseLocalePath(path)
  if (parsed) enResources[parsed.ns] = module
}

// lng -> ns -> lazy loader, for languages other than English.
const namespaceLoaders: Record<string, Record<string, () => Promise<ResourceKey>>> = {}
for (const [path, loader] of Object.entries(lazyLoaders)) {
  const parsed = parseLocalePath(path)
  if (parsed && parsed.lng !== "en") {
    (namespaceLoaders[parsed.lng] ??= {})[parsed.ns] = loader
  }
}

// English is bundled eagerly; mark it loaded so we never try to lazy-load it.
const loadedLanguages = new Set<string>(["en"])

async function loadLanguageResources(lng: string): Promise<void> {
  if (loadedLanguages.has(lng)) return
  const loaders = namespaceLoaders[lng]
  if (!loaders) return
  const bundles = await Promise.all(
    Object.entries(loaders).map(async ([ns, load]) => [ns, await load()] as const)
  )
  for (const [ns, data] of bundles) {
    i18n.addResourceBundle(lng, ns, data, true, true)
  }
  loadedLanguages.add(lng)
}

export const supportedLanguages = ["en", "zh-CN", "fr", "de", "it"] as const
export type AppLanguage = (typeof supportedLanguages)[number]
const LANGUAGE_STORAGE_KEY = "qui.language"

export const languageNames: Record<AppLanguage, string> = {
  en: "English",
  "zh-CN": "\u7B80\u4F53\u4E2D\u6587",
  fr: "Français",
  de: "Deutsch",
  it: "Italiano",
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

// Monotonic token so that rapid language switches can't apply out of order: a slow
// chunk load for an earlier selection must not overwrite a newer one once it finishes.
let latestLanguageRequest = 0

export async function changeLanguage(lng: AppLanguage) {
  persistLanguage(lng)
  const requestId = ++latestLanguageRequest
  await loadLanguageResources(lng)
  // A newer changeLanguage call superseded this one while its chunk was loading.
  if (requestId !== latestLanguageRequest) return
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

// Initialize synchronously with English so i18n.t works the moment this module is
// imported (lib helpers and tests rely on this). The active language, if not English,
// falls back to English until its chunk is loaded by initI18n().
i18n.use(initReactI18next).init({
  resources: { en: enResources },
  lng: getStoredLanguage() ?? detectBrowserLanguage() ?? "en",
  fallbackLng: "en",
  defaultNS: "common",
  ns: [...namespaces],
  interpolation: {
    escapeValue: false, // React already escapes
  },
})

// Ensure the active language's resources are loaded before the app renders, so users
// who picked a non-English language don't see an English flash on first paint. Await
// this in the entry point before mounting React. i18next set i18n.language from the
// stored/detected language above; English is already bundled.
export async function initI18n(): Promise<typeof i18n> {
  const active = i18n.language
  if (active && active !== "en" && !loadedLanguages.has(active)) {
    await loadLanguageResources(active)
    // Re-resolve so react-i18next switches from the English fallback to the now-loaded language.
    await i18n.changeLanguage(active)
  }
  return i18n
}

export default i18n
