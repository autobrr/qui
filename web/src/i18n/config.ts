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

export interface LanguageOption {
  code: AppLanguage
  labelKey: LanguageOptionKey
  nativeName: string
  locale: string
}

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

export const languageOptions: ReadonlyArray<LanguageOption> = [
  { code: "en", labelKey: "languageSwitcher.option.en", nativeName: "English", locale: "en" },
  { code: "zh-CN", labelKey: "languageSwitcher.option.zhCN", nativeName: "简体中文", locale: "zh-CN" },
  { code: "ja", labelKey: "languageSwitcher.option.ja", nativeName: "日本語", locale: "ja" },
  { code: "pt-BR", labelKey: "languageSwitcher.option.ptBR", nativeName: "Português (Brasil)", locale: "pt-BR" },
  { code: "de", labelKey: "languageSwitcher.option.de", nativeName: "Deutsch", locale: "de" },
  { code: "es-419", labelKey: "languageSwitcher.option.es419", nativeName: "Español (Latinoamérica)", locale: "es-419" },
  { code: "fr", labelKey: "languageSwitcher.option.fr", nativeName: "Français", locale: "fr" },
  { code: "ko", labelKey: "languageSwitcher.option.ko", nativeName: "한국어", locale: "ko" },
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
