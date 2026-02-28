import "i18next"
import { resources } from "./locales"

declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "common"
    resources: (typeof resources)["en"]
  }
}
