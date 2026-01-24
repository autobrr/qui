/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
import { useForm } from "@tanstack/react-form"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { Clock, Calendar, Globe } from "lucide-react"
import { usePersistedDateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import type { DateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import { formatTimestamp } from "@/lib/dateTimeUtils"
import { toast } from "sonner"

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
  ]
}

// Flatten all timezones into a single array
const ALL_TIMEZONES = Object.values(TIMEZONES_BY_REGION).flat()

// Get user's detected timezone
const userTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone

function SwitchSetting({
  label,
  checked,
  onCheckedChange,
  description,
}: {
  label: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  description?: string
}) {
  return (
    <div className="flex items-center gap-3">
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
      <div className="space-y-0.5">
        <Label className="text-sm font-medium">{label}</Label>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
    </div>
  )
}

export function DateTimePreferencesForm() {
  const { preferences, setPreferences } = usePersistedDateTimePreferences()
  const [previewKey, setPreviewKey] = React.useState(0) // Force preview updates

  const form = useForm({
    defaultValues: preferences,
    onSubmit: async ({ value }) => {
      try {
        setPreferences(value)
        toast.success("Date & time preferences updated successfully")
      } catch (error) {
        toast.error("Failed to update date & time preferences")
        console.error("Failed to update date & time preferences:", error)
      }
    },
  })

  // Update form when preferences change from other sources
  React.useEffect(() => {
    form.setFieldValue("timezone", preferences.timezone)
    form.setFieldValue("timeFormat", preferences.timeFormat)
    form.setFieldValue("dateFormat", preferences.dateFormat)
  }, [preferences, form])

  // Force preview update when form values change
  const updatePreview = React.useCallback(() => {
    setPreviewKey(prev => prev + 1)
  }, [])

  // Format example date for preview using current form values
  const getFormattedExample = () => {
    const now = new Date()
    const timeZone = (form.getFieldValue("timezone") || preferences.timezone) as DateTimePreferences["timezone"]
    const timeFormat = (form.getFieldValue("timeFormat") || preferences.timeFormat) as DateTimePreferences["timeFormat"]
    const dateFormat = (form.getFieldValue("dateFormat") || preferences.dateFormat) as DateTimePreferences["dateFormat"]

    const previewPreferences: DateTimePreferences = {
      timezone: timeZone,
      timeFormat,
      dateFormat,
    }

    return formatTimestamp(Math.floor(now.getTime() / 1000), previewPreferences)
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
      className="space-y-6"
    >
      {/* Timezone Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Globe className="h-4 w-4" />
          <h3 className="text-lg font-medium">Timezone</h3>
        </div>

        <form.Field name="timezone">
          {(field) => (
            <div className="space-y-2">
              <Label className="text-sm font-medium">Timezone</Label>
              <p className="text-xs text-muted-foreground">
                Select your local timezone for accurate time display
              </p>
              <Select
                value={field.state.value}
                onValueChange={(value) => {
                  field.handleChange(value)
                  updatePreview()
                }}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select timezone" />
                </SelectTrigger>
                <SelectContent className="max-h-96">
                  {/* Show user's detected timezone first if not in standard list */}
                  {userTimezone && !ALL_TIMEZONES.includes(userTimezone) && (
                    <>
                      <SelectItem key={userTimezone} value={userTimezone}>
                        <span className="font-medium">{userTimezone}</span> (detected)
                      </SelectItem>
                      <div className="border-t my-1" />
                    </>
                  )}
                  
                  {/* Group timezones by region */}
                  {Object.entries(TIMEZONES_BY_REGION).map(([region, timezones]) => (
                    <div key={region}>
                      <div className="px-2 py-1 text-xs font-semibold text-muted-foreground bg-muted/50">
                        {region}
                      </div>
                      {timezones.map((tz) => (
                        <SelectItem key={tz} value={tz} className="pl-4">
                          {tz.replace(/_/g, " ")}
                        </SelectItem>
                      ))}
                    </div>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
        </form.Field>
      </div>

      {/* Time Format Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Clock className="h-4 w-4" />
          <h3 className="text-lg font-medium">Time Format</h3>
        </div>

        <form.Field name="timeFormat">
          {(field) => (
            <SwitchSetting
              label="Use 12-hour format (AM/PM)"
              checked={field.state.value === "12h"}
              onCheckedChange={(checked) => {
                field.handleChange(checked ? "12h" : "24h")
                updatePreview()
              }}
              description="Toggle between 12-hour and 24-hour time display"
            />
          )}
        </form.Field>
      </div>

      {/* Date Format Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Calendar className="h-4 w-4" />
          <h3 className="text-lg font-medium">Date Format</h3>
        </div>

        <form.Field name="dateFormat">
          {(field) => (
            <div className="space-y-2">
              <Label className="text-sm font-medium">Date Format</Label>
              <p className="text-xs text-muted-foreground">
                Choose how dates are displayed throughout the application
              </p>
              <Select
                value={field.state.value}
                onValueChange={(value) => {
                  field.handleChange(value as "iso" | "us" | "eu" | "relative")
                  updatePreview()
                }}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select date format" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="iso">ISO 8601 (YYYY-MM-DD)</SelectItem>
                  <SelectItem value="us">US Format (MM/DD/YYYY)</SelectItem>
                  <SelectItem value="eu">European Format (DD/MM/YYYY)</SelectItem>
                  <SelectItem value="relative">Relative (e.g., "2 hours ago")</SelectItem>
                </SelectContent>
              </Select>
            </div>
          )}
        </form.Field>
      </div>

      {/* Preview Section */}
      <div className="space-y-2 p-4 bg-muted/30 rounded-lg">
        <Label className="text-sm font-medium">Preview</Label>
        <p key={previewKey} className="text-sm font-mono">{getFormattedExample()}</p>
        <p className="text-xs text-muted-foreground">
          This is how dates and times will appear in the application
        </p>
      </div>

      {/* Submit Button */}
      <div className="flex justify-end">
        <Button type="submit">
          Save Preferences
        </Button>
      </div>
    </form>
  )
}