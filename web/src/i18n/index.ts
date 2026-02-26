import i18n from "i18next"
import LanguageDetector from "i18next-browser-languagedetector"
import { initReactI18next } from "react-i18next"
import { fallbackLanguage, normalizeLanguage, supportedLanguages } from "./config"
import { resources } from "./resources"

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: fallbackLanguage,
    supportedLngs: supportedLanguages,
    defaultNS: "common",
    ns: ["common", "auth", "footer"],
    interpolation: {
      escapeValue: false,
    },
    detection: {
      order: ["localStorage", "navigator", "htmlTag"],
      lookupLocalStorage: "qui.language",
      caches: ["localStorage"],
      convertDetectedLanguage: (language) => normalizeLanguage(language),
    },
    react: {
      useSuspense: false,
    },
  })

const normalizedInitialLanguage = normalizeLanguage(i18n.resolvedLanguage || i18n.language)
if (normalizedInitialLanguage !== i18n.resolvedLanguage) {
  void i18n.changeLanguage(normalizedInitialLanguage)
}

document.documentElement.lang = normalizedInitialLanguage

i18n.on("languageChanged", (language) => {
  document.documentElement.lang = normalizeLanguage(language)
})

export default i18n
export * from "./config"
