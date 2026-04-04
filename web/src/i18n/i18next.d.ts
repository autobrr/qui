import "i18next"
import enAuth from "../locales/en/auth.json"
import enCommon from "../locales/en/common.json"
import enFooter from "../locales/en/footer.json"

type AppResources = {
  common: typeof enCommon
  auth: typeof enAuth
  footer: typeof enFooter
}

declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "common"
    resources: AppResources
  }
}
