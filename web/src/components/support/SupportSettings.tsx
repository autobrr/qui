/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Section } from "@/components/kit/Section"
import { SwitchRow } from "@/components/kit/SwitchRow"
import { Button } from "@/components/ui/button"
import { Heart } from "lucide-react"
import { useTranslation } from "react-i18next"
import { openSupport } from "./open"
import { setReminderOff, setShortcutHidden, useSupportReminder } from "./reminder"

/** The Support qui section of the Application tab. It brings back the heart and the reminder, and opens the dialog without the heart. */
export function SupportSettings() {
  const { t } = useTranslation("settings")
  const { settled, isSupporter, shortcutHidden, reminderOff } = useSupportReminder()

  return (
    <Section
      id="support"
      title={t("common:support.title")}
      description={t("application.support.description")}
      actions={
        <Button variant="outline" size="sm" onClick={openSupport}>
          <Heart className="fill-current text-red-500" />
          {isSupporter ? t("application.support.openSupporter") : t("application.support.open")}
        </Button>
      }
    >
      <SwitchRow
        label={t("application.support.shortcut")}
        help={t("application.support.shortcutHelp")}
        checked={!shortcutHidden}
        disabled={!settled}
        onCheckedChange={(checked) => setShortcutHidden(!checked)}
      />
      <SwitchRow
        label={t("application.support.reminder")}
        description={isSupporter ? t("application.support.reminderSupporter") : t("application.support.reminderDescription")}
        checked={!reminderOff && !shortcutHidden && !isSupporter}
        disabled={!settled || shortcutHidden || isSupporter}
        onCheckedChange={(checked) => setReminderOff(!checked)}
      />
    </Section>
  )
}
