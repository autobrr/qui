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

  if (!defaultNamespace) {
    return []
  }

  const localeCache = new Map()
  const missingKeys = new Set()
  const translationCallPattern = /\b(?:i18n\.)?t\(\s*"([^"]+)"(?:\s*,\s*(\{[\s\S]*?\}))?\s*\)/g

  function getLocale(namespace) {
    if (!localeCache.has(namespace)) {
      localeCache.set(namespace, loadLocale(namespace))
    }

    return localeCache.get(namespace)
  }

  for (const match of source.matchAll(translationCallPattern)) {
    const rawKey = match[1]
    const optionsSource = match[2]
    const { namespace, key } = resolveNamespaceAndKey(rawKey, optionsSource, defaultNamespace)

    if (!namespace) {
      continue
    }

    const locale = getLocale(namespace)
    if (!locale) {
      missingKeys.add(`${relativePath}: missing locale file for namespace "${namespace}"`)
      continue
    }

    if (!hasLocaleKey(locale, key)) {
      missingKeys.add(`${relativePath}: ${namespace}.${key}`)
    }
  }

  return [...missingKeys]
}
