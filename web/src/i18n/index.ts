import i18n from "i18next"
import { initReactI18next } from "react-i18next"
import { fallbackLanguage, normalizeLanguage, supportedLanguages, type AppLanguage } from "./config"

const namespaces = ["common", "auth", "footer"] as const
type AppNamespace = (typeof namespaces)[number]
type LocaleNamespaceModule = { default: Record<string, unknown> }

const localeModules = import.meta.glob<LocaleNamespaceModule>("../locales/*/*.json")

function getStoredLanguage(): string | null {
  if (typeof window === "undefined") {
    return null
  }

  try {
    return window.localStorage.getItem("qui.language")
  } catch {
    return null
  }
}

function persistLanguage(language: AppLanguage) {
  if (typeof window === "undefined") {
    return
  }

  try {
    window.localStorage.setItem("qui.language", language)
  } catch {
    // Ignore storage failures; the selected language still applies for this session.
  }
}

function detectInitialLanguage(): AppLanguage {
  const candidates = [
    getStoredLanguage(),
    ...(typeof navigator !== "undefined" ? navigator.languages : []),
    typeof navigator !== "undefined" ? navigator.language : null,
    typeof document !== "undefined" ? document.documentElement.lang : null,
  ]

  for (const candidate of candidates) {
    const normalized = normalizeLanguage(candidate)
    if (supportedLanguages.includes(normalized)) {
      return normalized
    }
  }

  return fallbackLanguage
}

async function loadNamespace(language: AppLanguage, namespace: AppNamespace) {
  const loader = localeModules[`../locales/${language}/${namespace}.json`]
  if (!loader) {
    throw new Error(`Missing locale module for ${language}/${namespace}`)
  }

  const module = await loader()
  return module.default
}

async function loadLanguageResources(language: AppLanguage) {
  const loadedNamespaces = await Promise.all(
    namespaces.map(async (namespace) => [namespace, await loadNamespace(language, namespace)] as const)
  )

  return Object.fromEntries(loadedNamespaces)
}

export async function ensureLanguageResources(language: string): Promise<AppLanguage> {
  const normalizedLanguage = normalizeLanguage(language)

  if (namespaces.every((namespace) => i18n.hasResourceBundle(normalizedLanguage, namespace))) {
    return normalizedLanguage
  }

  const resources = await loadLanguageResources(normalizedLanguage)

  for (const namespace of namespaces) {
    const resource = resources[namespace]
    i18n.addResourceBundle(normalizedLanguage, namespace, resource, true, true)
  }

  return normalizedLanguage
}

async function initializeI18n() {
  const initialLanguage = detectInitialLanguage()
  const fallbackResources = await loadLanguageResources(fallbackLanguage)
  const initialResources = initialLanguage === fallbackLanguage ? {
    [fallbackLanguage]: fallbackResources,
  } : {
    [fallbackLanguage]: fallbackResources,
    [initialLanguage]: await loadLanguageResources(initialLanguage),
  }

  await i18n
    .use(initReactI18next)
    .init({
      resources: initialResources,
      lng: initialLanguage,
      fallbackLng: fallbackLanguage,
      supportedLngs: supportedLanguages,
      defaultNS: "common",
      ns: namespaces,
      interpolation: {
        escapeValue: false,
      },
      react: {
        useSuspense: false,
      },
    })

  persistLanguage(initialLanguage)

  if (typeof document !== "undefined") {
    document.documentElement.lang = initialLanguage
  }
}

export const i18nReady = initializeI18n()

export async function setAppLanguage(language: string) {
  const normalizedLanguage = await ensureLanguageResources(language)
  await i18n.changeLanguage(normalizedLanguage)
}

i18n.on("languageChanged", (language) => {
  const normalizedLanguage = normalizeLanguage(language)
  persistLanguage(normalizedLanguage)

  if (typeof document !== "undefined") {
    document.documentElement.lang = normalizedLanguage
  }
})

export default i18n
export * from "./config"
