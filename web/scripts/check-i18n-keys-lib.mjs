function parseNamespaces(source) {
  const namespaces = []

  for (const match of source.matchAll(/useTranslation\(\s*(?:"([^"]+)"|\[([^\]]+)\])\s*\)/g)) {
    const singleNamespace = match[1]
    const namespaceList = match[2]

    if (singleNamespace) {
      namespaces.push(singleNamespace)
      continue
    }

    if (!namespaceList) {
      continue
    }

    for (const namespaceMatch of namespaceList.matchAll(/"([^"]+)"/g)) {
      namespaces.push(namespaceMatch[1])
    }
  }

  return [...new Set(namespaces)]
}

function getNestedValue(obj, key) {
  return key.split(".").reduce((current, part) => {
    if (current && Object.prototype.hasOwnProperty.call(current, part)) {
      return current[part]
    }

    return undefined
  }, obj)
}

function hasLocaleKey(locale, key) {
  if (getNestedValue(locale, key) !== undefined) {
    return true
  }

  return (
    getNestedValue(locale, `${key}_one`) !== undefined ||
    getNestedValue(locale, `${key}_other`) !== undefined
  )
}

function resolveNamespaceAndKey(rawKey, optionsSource, defaultNamespace) {
  if (rawKey.includes(":")) {
    const separatorIndex = rawKey.indexOf(":")
    return {
      namespace: rawKey.slice(0, separatorIndex),
      key: rawKey.slice(separatorIndex + 1),
    }
  }

  const namespaceOverride = optionsSource?.match(/\bns:\s*"([^"]+)"/)?.[1]
  return {
    namespace: namespaceOverride ?? defaultNamespace,
    key: rawKey,
  }
}

export function collectMissingKeysForSource({
  source,
  relativePath,
  loadLocale,
}) {
  const namespaces = parseNamespaces(source)
  const defaultNamespace = namespaces[0]
  const directiveNamespace = source.match(/^\s*\/\/\s*i18n-namespace:\s*(\S+)/m)?.[1]
  // A route file holds one staticData title, so its titleNs covers every titleKey in the file.
  const titleNamespace = source.match(/\btitleNs:\s*"([^"]+)"/)?.[1]

  const localeCache = new Map()
  const missingKeys = new Set()
  const keyPropertyPattern = /\b(labelKey|titleKey|placeholderKey|descriptionKey):\s*"([^"]+)"/g
  const translationCallPattern = /\b(?:i18n\.)?t\(\s*"([^"]+)"(?:\s*,\s*(\{[\s\S]*?\}))?\s*\)/g

  function getLocale(namespace) {
    if (!localeCache.has(namespace)) {
      localeCache.set(namespace, loadLocale(namespace))
    }

    return localeCache.get(namespace)
  }

  function checkKey(namespace, key) {
    const locale = getLocale(namespace)
    if (!locale) {
      missingKeys.add(`${relativePath}: missing locale file for namespace "${namespace}"`)
      return
    }

    if (!hasLocaleKey(locale, key)) {
      missingKeys.add(`${relativePath}: ${namespace}.${key}`)
    }
  }

  if (directiveNamespace && defaultNamespace) {
    missingKeys.add(`${relativePath}: remove "// i18n-namespace: ${directiveNamespace}"; useTranslation sets the namespace`)
  }

  // Data tables hand these values to t() later, so the t("...") scan below never sees them.
  for (const match of source.matchAll(keyPropertyPattern)) {
    const lineStart = source.lastIndexOf("\n", match.index) + 1
    if (source.slice(lineStart, match.index).trimStart().startsWith("//")) {
      continue
    }

    const [, property, rawKey] = match
    const fileNamespace = (property === "titleKey" && titleNamespace) || defaultNamespace || directiveNamespace
    const { namespace, key } = resolveNamespaceAndKey(rawKey, undefined, fileNamespace)
    if (!namespace) {
      missingKeys.add(`${relativePath}: key properties have no namespace; add "// i18n-namespace: <ns>" to the file`)
      continue
    }

    checkKey(namespace, key)
  }

  if (!defaultNamespace) {
    return [...missingKeys]
  }

  for (const match of source.matchAll(translationCallPattern)) {
    const rawKey = match[1]
    const optionsSource = match[2]
    const { namespace, key } = resolveNamespaceAndKey(rawKey, optionsSource, defaultNamespace)

    if (!namespace) {
      continue
    }

    checkKey(namespace, key)
  }

  return [...missingKeys]
}
