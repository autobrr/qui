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

import zhCNCommon from "./locales/zh-CN/common.json"
import zhCNAuth from "./locales/zh-CN/auth.json"
import zhCNSettings from "./locales/zh-CN/settings.json"
import zhCNTorrents from "./locales/zh-CN/torrents.json"
import zhCNDashboard from "./locales/zh-CN/dashboard.json"
import zhCNCrossseed from "./locales/zh-CN/crossseed.json"
import zhCNRss from "./locales/zh-CN/rss.json"
import zhCNSearch from "./locales/zh-CN/search.json"
import zhCNInstances from "./locales/zh-CN/instances.json"
import zhCNAutomations from "./locales/zh-CN/automations.json"

import frCommon from "./locales/fr/common.json"
import frAuth from "./locales/fr/auth.json"
import frSettings from "./locales/fr/settings.json"
import frTorrents from "./locales/fr/torrents.json"
import frDashboard from "./locales/fr/dashboard.json"
import frCrossseed from "./locales/fr/crossseed.json"
import frRss from "./locales/fr/rss.json"
import frSearch from "./locales/fr/search.json"
import frInstances from "./locales/fr/instances.json"
import frAutomations from "./locales/fr/automations.json"

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
    "zh-CN": {
      common: zhCNCommon,
      auth: zhCNAuth,
      settings: zhCNSettings,
      torrents: zhCNTorrents,
      dashboard: zhCNDashboard,
      crossseed: zhCNCrossseed,
      rss: zhCNRss,
      search: zhCNSearch,
      instances: zhCNInstances,
      automations: zhCNAutomations,
    },
    fr: {
      common: frCommon,
      auth: frAuth,
      settings: frSettings,
      torrents: frTorrents,
      dashboard: frDashboard,
      crossseed: frCrossseed,
      rss: frRss,
      search: frSearch,
      instances: frInstances,
      automations: frAutomations,
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
