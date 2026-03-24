import deAuth from "../locales/de/auth.json"
import deCommon from "../locales/de/common.json"
import deFooter from "../locales/de/footer.json"
import enAuth from "../locales/en/auth.json"
import enCommon from "../locales/en/common.json"
import enFooter from "../locales/en/footer.json"
import es419Auth from "../locales/es-419/auth.json"
import es419Common from "../locales/es-419/common.json"
import es419Footer from "../locales/es-419/footer.json"
import frAuth from "../locales/fr/auth.json"
import frCommon from "../locales/fr/common.json"
import frFooter from "../locales/fr/footer.json"
import jaAuth from "../locales/ja/auth.json"
import jaCommon from "../locales/ja/common.json"
import jaFooter from "../locales/ja/footer.json"
import koAuth from "../locales/ko/auth.json"
import koCommon from "../locales/ko/common.json"
import koFooter from "../locales/ko/footer.json"
import ptBRAuth from "../locales/pt-BR/auth.json"
import ptBRCommon from "../locales/pt-BR/common.json"
import ptBRFooter from "../locales/pt-BR/footer.json"
import zhCNAuth from "../locales/zh-CN/auth.json"
import zhCNCommon from "../locales/zh-CN/common.json"
import zhCNFooter from "../locales/zh-CN/footer.json"

export const resources = {
  en: {
    common: enCommon,
    auth: enAuth,
    footer: enFooter,
  },
  "zh-CN": {
    common: zhCNCommon,
    auth: zhCNAuth,
    footer: zhCNFooter,
  },
  ja: {
    common: jaCommon,
    auth: jaAuth,
    footer: jaFooter,
  },
  "pt-BR": {
    common: ptBRCommon,
    auth: ptBRAuth,
    footer: ptBRFooter,
  },
  de: {
    common: deCommon,
    auth: deAuth,
    footer: deFooter,
  },
  "es-419": {
    common: es419Common,
    auth: es419Auth,
    footer: es419Footer,
  },
  fr: {
    common: frCommon,
    auth: frAuth,
    footer: frFooter,
  },
  ko: {
    common: koCommon,
    auth: koAuth,
    footer: koFooter,
  },
} as const
