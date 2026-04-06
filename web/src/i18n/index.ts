/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import i18n from "i18next"
import { initReactI18next } from "react-i18next"

import common from "./locales/en/common.json"
import auth from "./locales/en/auth.json"
import settings from "./locales/en/settings.json"
import torrents from "./locales/en/torrents.json"
import dashboard from "./locales/en/dashboard.json"
import crossseed from "./locales/en/crossseed.json"
import rss from "./locales/en/rss.json"
import search from "./locales/en/search.json"
import instances from "./locales/en/instances.json"
import automations from "./locales/en/automations.json"

export const supportedLanguages = ["en"] as const
export type AppLanguage = (typeof supportedLanguages)[number]
const LANGUAGE_STORAGE_KEY = "qui.language"

export const languageNames: Record<AppLanguage, string> = {
  en: "English",
}

function isAppLanguage(value: string | null): value is AppLanguage {
  return value !== null && supportedLanguages.includes(value as AppLanguage)
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
  resources: {
    en: {
      common,
      auth,
      settings,
      torrents,
      dashboard,
      crossseed,
      rss,
      search,
      instances,
      automations,
    },
  },
  lng: getStoredLanguage() ?? "en",
  fallbackLng: "en",
  defaultNS: "common",
  ns: [...namespaces],
  interpolation: {
    escapeValue: false, // React already escapes
  },
})

export default i18n
