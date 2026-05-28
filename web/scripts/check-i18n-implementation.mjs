import fs from "node:fs"
import path from "node:path"

const webRoot = path.resolve(import.meta.dirname, "..")

const checks = [
  {
    file: "src/i18n/index.ts",
    forbidden: [
      "lng: localStorage.getItem(",
      "export function changeLanguage(lng: AppLanguage) {\n  localStorage.setItem(",
    ],
    required: [
      "function getStoredLanguage()",
      "function persistLanguage(",
      "lng: getStoredLanguage() ?? ",
      "persistLanguage(lng)",
    ],
    message: "i18n bootstrap must route storage access through guarded helpers",
  },
  {
    file: "src/hooks/useDateTimeFormatters.ts",
    required: [
      "useMemo",
      "const { i18n } = useTranslation()",
      "i18n.resolvedLanguage",
    ],
    message: "date/time formatter hook must memoize formatter callbacks and react to language changes",
  },
]

const failures = []

for (const check of checks) {
  const filePath = path.join(webRoot, check.file)
  const source = fs.readFileSync(filePath, "utf8")

  for (const pattern of check.forbidden ?? []) {
    if (source.includes(pattern)) {
      failures.push(`${check.file}: ${check.message} (${pattern})`)
    }
  }

  for (const pattern of check.required ?? []) {
    if (!source.includes(pattern)) {
      failures.push(`${check.file}: ${check.message} (missing ${pattern})`)
    }
  }
}

if (failures.length > 0) {
  console.error("i18n implementation guard failures:\n")
  for (const failure of failures) {
    console.error(`- ${failure}`)
  }
  process.exit(1)
}

console.log("i18n implementation guards passed.")
