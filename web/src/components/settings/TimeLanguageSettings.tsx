/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTranslation } from "react-i18next"

import { Facts } from "@/components/kit/Facts"
import { Field } from "@/components/kit/Field"
import { Section } from "@/components/kit/Section"
import { SwitchRow } from "@/components/kit/SwitchRow"
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectSeparator, SelectTrigger, SelectValue } from "@/components/ui/select"
import { usePersistedDateTimePreferences, type DateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import { changeLanguage, languageNames, supportedLanguages, type AppLanguage } from "@/i18n"
import { useClientSettingsSettled } from "@/lib/client-settings"
import { formatTimestamp } from "@/lib/dateTimeUtils"

// Comprehensive worldwide timezone list organized by region
const TIMEZONES_BY_REGION = {
  "UTC": ["UTC"],
  "Americas": [
    "America/New_York",      // Eastern Time
    "America/Chicago",       // Central Time
    "America/Denver",        // Mountain Time
    "America/Los_Angeles",   // Pacific Time
    "America/Anchorage",     // Alaska Time
    "America/Honolulu",      // Hawaii Time
    "America/Toronto",       // Eastern Canada
    "America/Vancouver",     // Pacific Canada
    "America/Montreal",      // Eastern Canada
    "America/Sao_Paulo",     // Brazil
    "America/Argentina/Buenos_Aires", // Argentina
    "America/Mexico_City",   // Mexico
    "America/Lima",          // Peru
    "America/Bogota",        // Colombia
    "America/Caracas",       // Venezuela
    "America/Santiago",      // Chile
    "America/Havana",        // Cuba
    "America/Jamaica",       // Jamaica
    "America/Panama",        // Panama
    "America/Guatemala",     // Guatemala
  ],
  "Europe": [
    "Europe/London",         // GMT/BST
    "Europe/Dublin",         // Ireland
    "Europe/Paris",          // Central European Time
    "Europe/Berlin",         // Germany
    "Europe/Rome",           // Italy
    "Europe/Madrid",         // Spain
    "Europe/Amsterdam",      // Netherlands
    "Europe/Brussels",       // Belgium
    "Europe/Vienna",         // Austria
    "Europe/Zurich",         // Switzerland
    "Europe/Stockholm",      // Sweden
    "Europe/Copenhagen",     // Denmark
    "Europe/Oslo",           // Norway
    "Europe/Helsinki",       // Finland
    "Europe/Warsaw",         // Poland
    "Europe/Prague",         // Czech Republic
    "Europe/Budapest",       // Hungary
    "Europe/Bucharest",      // Romania
    "Europe/Sofia",          // Bulgaria
    "Europe/Athens",         // Greece
    "Europe/Moscow",         // Russia (MSK)
    "Europe/Kiev",           // Ukraine
    "Europe/Istanbul",       // Turkey
    "Europe/Lisbon",         // Portugal
  ],
  "Asia": [
    "Asia/Tokyo",            // Japan
    "Asia/Shanghai",         // China
    "Asia/Hong_Kong",        // Hong Kong
    "Asia/Singapore",        // Singapore
    "Asia/Seoul",            // South Korea
    "Asia/Taipei",           // Taiwan
    "Asia/Bangkok",          // Thailand
    "Asia/Jakarta",          // Indonesia
    "Asia/Manila",           // Philippines
    "Asia/Kuala_Lumpur",     // Malaysia
    "Asia/Ho_Chi_Minh",      // Vietnam
    "Asia/Kolkata",          // India
    "Asia/Karachi",          // Pakistan
    "Asia/Dhaka",            // Bangladesh
    "Asia/Colombo",          // Sri Lanka
    "Asia/Kathmandu",        // Nepal
    "Asia/Dubai",            // UAE
    "Asia/Qatar",            // Qatar
    "Asia/Kuwait",           // Kuwait
    "Asia/Riyadh",           // Saudi Arabia
    "Asia/Tehran",           // Iran
    "Asia/Baku",             // Azerbaijan
    "Asia/Tashkent",         // Uzbekistan
    "Asia/Almaty",           // Kazakhstan
    "Asia/Novosibirsk",      // Russia (NOVT)
    "Asia/Vladivostok",      // Russia (VLAT)
    "Asia/Yekaterinburg",    // Russia (YEKT)
    "Asia/Jerusalem",        // Israel
    "Asia/Beirut",           // Lebanon
  ],
  "Africa": [
    "Africa/Cairo",          // Egypt
    "Africa/Lagos",          // Nigeria
    "Africa/Johannesburg",   // South Africa
    "Africa/Nairobi",        // Kenya
    "Africa/Casablanca",     // Morocco
    "Africa/Algiers",        // Algeria
    "Africa/Tunis",          // Tunisia
    "Africa/Tripoli",        // Libya
    "Africa/Khartoum",       // Sudan
    "Africa/Addis_Ababa",    // Ethiopia
    "Africa/Dar_es_Salaam",  // Tanzania
    "Africa/Kampala",        // Uganda
    "Africa/Kinshasa",       // DR Congo
    "Africa/Luanda",         // Angola
    "Africa/Maputo",         // Mozambique
    "Africa/Harare",         // Zimbabwe
    "Africa/Lusaka",         // Zambia
    "Africa/Accra",          // Ghana
    "Africa/Abidjan",        // Ivory Coast
    "Africa/Dakar",          // Senegal
  ],
  "Oceania": [
    "Australia/Sydney",      // Eastern Australia
    "Australia/Melbourne",   // Eastern Australia
    "Australia/Brisbane",    // Eastern Australia (no DST)
    "Australia/Perth",       // Western Australia
    "Australia/Adelaide",    // Central Australia
    "Australia/Darwin",      // Northern Territory
    "Australia/Hobart",      // Tasmania
    "Pacific/Auckland",      // New Zealand
    "Pacific/Wellington",    // New Zealand
    "Pacific/Fiji",          // Fiji
    "Pacific/Tahiti",        // Tahiti
    "Pacific/Honolulu",      // Hawaii (also in Americas)
    "Pacific/Guam",          // Guam
    "Pacific/Port_Moresby",  // Papua New Guinea
  ],
}

const ALL_TIMEZONES = Object.values(TIMEZONES_BY_REGION).flat()

const DETECTED_TIMEZONE = Intl.DateTimeFormat().resolvedOptions().timeZone

const DATE_FORMATS = [
  { value: "iso", labelKey: "dateTime.isoFormat" },
  { value: "us", labelKey: "dateTime.usFormat" },
  { value: "eu", labelKey: "dateTime.euFormat" },
  { value: "relative", labelKey: "dateTime.relativeFormat" },
] as const satisfies readonly { value: DateTimePreferences["dateFormat"]; labelKey: string }[]

/** The Time & Language tab: the display language, and how qui shows dates and times. */
export function TimeLanguageSettings() {
  return (
    <>
      <LanguageSection />
      <DateTimeSection />
    </>
  )
}

function LanguageSection() {
  const { t, i18n } = useTranslation("settings")

  return (
    <Section
      title={t("language.cardTitle")}
      description={t("language.cardDescription")}
      actions={
        <Select value={i18n.language} onValueChange={(value) => void changeLanguage(value as AppLanguage)}>
          <SelectTrigger aria-label={t("language.cardTitle")} className="w-48 max-sm:data-[size=default]:h-10">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {supportedLanguages.map((language) => (
              <SelectItem key={language} value={language}>{languageNames[language]}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      }
    />
  )
}

function DateTimeSection() {
  const { t } = useTranslation("settings")
  const { preferences, setPreferences } = usePersistedDateTimePreferences()
  // Until the server answers, the controls show the defaults, and a change would be lost.
  const settled = useClientSettingsSettled()

  return (
    <Section title={t("dateTime.cardTitle")} description={t("dateTime.cardDescription")}>
      <div className="grid gap-6 sm:grid-cols-2">
        <Field label={t("dateTime.timezone")}>
          {(control) => (
            <Select value={preferences.timezone} disabled={!settled} onValueChange={(timezone) => setPreferences({ timezone })}>
              <SelectTrigger {...control} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent className="max-h-96">
                {DETECTED_TIMEZONE && !ALL_TIMEZONES.includes(DETECTED_TIMEZONE) && (
                  <>
                    <SelectItem value={DETECTED_TIMEZONE}>
                      {DETECTED_TIMEZONE.replace(/_/g, " ")} ({t("dateTime.detected")})
                    </SelectItem>
                    <SelectSeparator />
                  </>
                )}
                {Object.entries(TIMEZONES_BY_REGION).map(([region, timezones]) => (
                  <SelectGroup key={region}>
                    <SelectLabel>{t(`dateTime.regions.${region}`)}</SelectLabel>
                    {timezones.map((timezone) => (
                      <SelectItem key={timezone} value={timezone}>{timezone.replace(/_/g, " ")}</SelectItem>
                    ))}
                  </SelectGroup>
                ))}
              </SelectContent>
            </Select>
          )}
        </Field>
        <Field label={t("dateTime.dateFormat")}>
          {(control) => (
            <Select
              value={preferences.dateFormat}
              disabled={!settled}
              onValueChange={(dateFormat) => setPreferences({ dateFormat: dateFormat as DateTimePreferences["dateFormat"] })}
            >
              <SelectTrigger {...control} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {DATE_FORMATS.map((format) => (
                  <SelectItem key={format.value} value={format.value}>{t(format.labelKey)}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </Field>
      </div>
      <SwitchRow
        label={t("dateTime.use12Hour")}
        checked={preferences.timeFormat === "12h"}
        disabled={!settled}
        onCheckedChange={(checked) => setPreferences({ timeFormat: checked ? "12h" : "24h" })}
      />
      <Facts
        items={[{
          label: t("dateTime.preview"),
          value: formatTimestamp(Math.floor(Date.now() / 1000), preferences, true),
          hint: t("dateTime.previewDescription"),
          mono: true,
        }]}
      />
    </Section>
  )
}
