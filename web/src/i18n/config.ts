export const supportedLanguages = ["en", "zh-CN", "ja", "pt-BR", "de", "es-419", "fr", "ko"] as const

export type AppLanguage = (typeof supportedLanguages)[number]

export const fallbackLanguage: AppLanguage = "en"

type LanguageOptionKey =
  | "languageSwitcher.option.en"
  | "languageSwitcher.option.zhCN"
  | "languageSwitcher.option.ja"
  | "languageSwitcher.option.ptBR"
  | "languageSwitcher.option.de"
  | "languageSwitcher.option.es419"
  | "languageSwitcher.option.fr"
  | "languageSwitcher.option.ko"

const languageAliases: Record<string, AppLanguage> = {
  en: "en",
  "en-us": "en",
  "en-gb": "en",
  zh: "zh-CN",
  "zh-cn": "zh-CN",
  "zh-hans": "zh-CN",
  ja: "ja",
  "ja-jp": "ja",
  pt: "pt-BR",
  "pt-br": "pt-BR",
  de: "de",
  "de-de": "de",
  es: "es-419",
  "es-419": "es-419",
  "es-es": "es-419",
  "es-mx": "es-419",
  fr: "fr",
  "fr-fr": "fr",
  "fr-ca": "fr",
  ko: "ko",
  "ko-kr": "ko",
}

export const languageOptions: ReadonlyArray<{
  code: AppLanguage
  labelKey: LanguageOptionKey
}> = [
  { code: "en", labelKey: "languageSwitcher.option.en" },
  { code: "zh-CN", labelKey: "languageSwitcher.option.zhCN" },
  { code: "ja", labelKey: "languageSwitcher.option.ja" },
  { code: "pt-BR", labelKey: "languageSwitcher.option.ptBR" },
  { code: "de", labelKey: "languageSwitcher.option.de" },
  { code: "es-419", labelKey: "languageSwitcher.option.es419" },
  { code: "fr", labelKey: "languageSwitcher.option.fr" },
  { code: "ko", labelKey: "languageSwitcher.option.ko" },
]

export function normalizeLanguage(language?: string | null): AppLanguage {
  if (!language) {
    return fallbackLanguage
  }

  const normalizedLanguage = language.toLowerCase()

  if (normalizedLanguage in languageAliases) {
    return languageAliases[normalizedLanguage]
  }

  if (normalizedLanguage.startsWith("zh")) {
    return "zh-CN"
  }

  if (normalizedLanguage.startsWith("ja")) {
    return "ja"
  }

  if (normalizedLanguage.startsWith("pt")) {
    return "pt-BR"
  }

  if (normalizedLanguage.startsWith("de")) {
    return "de"
  }

  if (normalizedLanguage.startsWith("es")) {
    return "es-419"
  }

  if (normalizedLanguage.startsWith("fr")) {
    return "fr"
  }

  if (normalizedLanguage.startsWith("ko")) {
    return "ko"
  }

  if (normalizedLanguage.startsWith("en")) {
    return "en"
  }

  return fallbackLanguage
}
