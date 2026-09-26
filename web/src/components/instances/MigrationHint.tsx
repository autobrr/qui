/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cn } from "@/lib/utils"
import { Trans } from "react-i18next"

export function MigrationHint({ className }: { className?: string }) {
  return (
    <p className={cn("text-xs text-muted-foreground", className)}>
      <Trans
        ns="instances"
        i18nKey="migrationHint"
        components={{
          docs: (
            <a
              href="https://getqui.com/docs/features/client-migration"
              target="_blank"
              rel="noopener noreferrer"
              className="underline underline-offset-2 hover:text-foreground"
            />
          ),
        }}
      />
    </p>
  )
}
