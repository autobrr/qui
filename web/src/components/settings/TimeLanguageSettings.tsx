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

const TIMEZONES_BY_REGION = {
  "UTC": ["UTC"],
  "Americas": [
    "America/New_York",
    "America/Chicago",
    "America/Denver",
    "America/Los_Angeles",
    "America/Anchorage",
    "America/Honolulu",
    "America/Toronto",
    "America/Vancouver",
    "America/Montreal",
    "America/Sao_Paulo",
    "America/Argentina/Buenos_Aires",
    "America/Mexico_City",
    "America/Lima",
    "America/Bogota",
    "America/Caracas",
    "America/Santiago",
    "America/Havana",
    "America/Jamaica",
    "America/Panama",
    "America/Guatemala",
  ],
  "Europe": [
    "Europe/London",
    "Europe/Dublin",
    "Europe/Paris",
    "Europe/Berlin",
    "Europe/Rome",
    "Europe/Madrid",
    "Europe/Amsterdam",
    "Europe/Brussels",
    "Europe/Vienna",
    "Europe/Zurich",
    "Europe/Stockholm",
    "Europe/Copenhagen",
    "Europe/Oslo",
    "Europe/Helsinki",
    "Europe/Warsaw",
    "Europe/Prague",
    "Europe/Budapest",
    "Europe/Bucharest",
    "Europe/Sofia",
    "Europe/Athens",
    "Europe/Moscow",
    "Europe/Kiev",
    "Europe/Istanbul",
    "Europe/Lisbon",
  ],
  "Asia": [
    "Asia/Tokyo",
    "Asia/Shanghai",
    "Asia/Hong_Kong",
    "Asia/Singapore",
    "Asia/Seoul",
    "Asia/Taipei",
    "Asia/Bangkok",
    "Asia/Jakarta",
    "Asia/Manila",
    "Asia/Kuala_Lumpur",
    "Asia/Ho_Chi_Minh",
    "Asia/Kolkata",
    "Asia/Karachi",
    "Asia/Dhaka",
    "Asia/Colombo",
    "Asia/Kathmandu",
    "Asia/Dubai",
    "Asia/Qatar",
    "Asia/Kuwait",
    "Asia/Riyadh",
    "Asia/Tehran",
    "Asia/Baku",
    "Asia/Tashkent",
    "Asia/Almaty",
    "Asia/Novosibirsk",
    "Asia/Vladivostok",
    "Asia/Yekaterinburg",
    "Asia/Jerusalem",
    "Asia/Beirut",
  ],
  "Africa": [
    "Africa/Cairo",
    "Africa/Lagos",
    "Africa/Johannesburg",
    "Africa/Nairobi",
    "Africa/Casablanca",
    "Africa/Algiers",
    "Africa/Tunis",
    "Africa/Tripoli",
    "Africa/Khartoum",
    "Africa/Addis_Ababa",
    "Africa/Dar_es_Salaam",
    "Africa/Kampala",
    "Africa/Kinshasa",
    "Africa/Luanda",
    "Africa/Maputo",
    "Africa/Harare",
    "Africa/Lusaka",
    "Africa/Accra",
    "Africa/Abidjan",
    "Africa/Dakar",
  ],
  "Oceania": [
    "Australia/Sydney",
    "Australia/Melbourne",
    "Australia/Brisbane",
    "Australia/Perth",
    "Australia/Adelaide",
    "Australia/Darwin",
    "Australia/Hobart",
    "Pacific/Auckland",
    "Pacific/Wellington",
    "Pacific/Fiji",
    "Pacific/Tahiti",
    "Pacific/Honolulu",
    "Pacific/Guam",
    "Pacific/Port_Moresby",
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
