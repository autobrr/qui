/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useForm } from "@tanstack/react-form"
import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import type { AppPreferences } from "@/types"

interface UsePreferencesFormOptions<TValues> {
  instanceId: number
  preferences: AppPreferences
  toForm: (preferences: AppPreferences) => TValues
  // Omit when the form values are the payload.
  toPayload?: (values: TValues) => Partial<AppPreferences>
  i18nPrefix: string
  onSaved?: (values: TValues) => void
}

export function usePreferencesForm<TValues extends Partial<AppPreferences>>({
  instanceId,
  preferences,
  toForm,
  toPayload,
  i18nPrefix,
  onSaved,
}: UsePreferencesFormOptions<TValues>) {
  const { t } = useTranslation("instances")
  const { updatePreferences, isUpdating } = useInstancePreferences(instanceId)
  // Frozen: TanStack Form resets an untouched form when defaultValues changes, so a preferences refresh would rewrite an open form.
  const [defaultValues] = useState(() => toForm(preferences))
  const payloadOf = (values: TValues): Partial<AppPreferences> => toPayload ? toPayload(values) : values
  // The payload of the last successful save. A save sends only the keys that differ from it, or from the starting values for a key it lacks.
  const lastSaved = useRef<Partial<AppPreferences>>({})

  const form = useForm({
    defaultValues,
    onSubmit: async ({ value }) => {
      // Built at each save: a toPayload that reads the instance capabilities adds keys once they load.
      const baseline = { ...payloadOf(defaultValues), ...lastSaved.current }
      const payload = payloadOf(value)
      const changed = Object.fromEntries(Object.entries(payload).filter(
        ([key, next]) => JSON.stringify(next) !== JSON.stringify(baseline[key as keyof AppPreferences])
      )) as Partial<AppPreferences>
      try {
        if (Object.keys(changed).length > 0) {
          await updatePreferences(changed)
        }
      } catch (error) {
        console.error(`Failed to save ${i18nPrefix}:`, error)
        toast.error(t(`${i18nPrefix}.toast.error`))
        return
      }
      lastSaved.current = payload
      toast.success(t(`${i18nPrefix}.toast.success`))
      onSaved?.(value)
    },
  })

  return { form, isUpdating }
}
