/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { InstallUpdateButton } from "@/components/settings/InstallUpdateButton"
import { RestartOverlay } from "@/components/settings/RestartOverlay"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useSelfUpdate } from "@/hooks/useSelfUpdate"
import { api } from "@/lib/api"
import { cn, collapseRowClassName } from "@/lib/utils"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Download, X } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

/** The update notice of the sidebar. With iconOnly, it is one icon that opens the Install dialog in Settings. */
export function UpdateBanner({ iconOnly = false }: { iconOnly?: boolean }) {
  const { t } = useTranslation("common")
  const [dismissed, setDismissed] = useState(false)
  const selfUpdate = useSelfUpdate()

  const { data: updateInfo } = useQuery({
    queryKey: ["latest-version"],
    queryFn: () => api.getLatestVersion(),
    // Check for updates every 2 minutes
    refetchInterval: 2 * 60 * 1000,
    // Don't show loading state on mount, run in background
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })

  const bannerButtonClass = "h-6 text-xs border-green-300 text-green-700 hover:bg-green-100 dark:border-green-700 dark:text-green-300 dark:hover:bg-green-900"

  return (
    <>
      {!dismissed && updateInfo && (
        <div>
          <div inert={iconOnly} className={collapseRowClassName(iconOnly)}>
            <div className="min-h-0 overflow-hidden">
              {/* The card keeps its full width while the sidebar gets narrow, so its text does not wrap again. */}
              <div className={cn(
                "mb-3 rounded-md border border-green-200 bg-green-50 p-3",
                "dark:border-green-800 dark:bg-green-950/50",
                iconOnly && "w-58"
              )}>
                <div className="flex items-start gap-2">
                  <Download className="h-4 w-4 text-green-600 dark:text-green-400 mt-0.5 flex-shrink-0" />
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-green-800 dark:text-green-200">
                      {t("updateBanner.updateAvailable")}
                    </p>
                    <p className="text-xs text-green-700 dark:text-green-300 mt-1">
                      {t("updateBanner.versionAvailable", { version: updateInfo.tag_name })}
                    </p>
                    {/* The Install dialog links the release notes, so one button is enough. */}
                    <div className="mt-2">
                      <InstallUpdateButton selfUpdate={selfUpdate} release={updateInfo} className={bannerButtonClass} />
                    </div>
                  </div>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="h-4 w-4 text-green-600 hover:text-green-800 dark:text-green-400 dark:hover:text-green-200"
                    onClick={() => setDismissed(true)}
                  >
                    <X className="h-3 w-3" />
                    <span className="sr-only">{t("updateBanner.dismiss")}</span>
                  </Button>
                </div>
              </div>
            </div>
          </div>
          <div inert={!iconOnly} className={collapseRowClassName(!iconOnly)}>
            <div className="min-h-0 overflow-hidden">
              <Tooltip>
                <TooltipTrigger asChild>
                  <Link
                    to="/settings"
                    search={{ tab: "application", modal: "install-update" }}
                    aria-label={t("updateBanner.updateAvailable")}
                    className="mb-3 flex h-8 items-center rounded-md px-3 text-green-600 -outline-offset-2 hover:bg-sidebar-accent dark:text-green-400 short:h-7"
                  >
                    <Download className="h-4 w-4" />
                  </Link>
                </TooltipTrigger>
                <TooltipContent side="right" className="text-xs">
                  {t("updateBanner.updateAvailable")}
                  <span className="block opacity-70">{t("updateBanner.versionAvailable", { version: updateInfo.tag_name })}</span>
                </TooltipContent>
              </Tooltip>
            </div>
          </div>
        </div>
      )}
      {/* Outside the banner: while qui is down, the update check fails and
          updateInfo turns null. */}
      <RestartOverlay state={selfUpdate.overlay} update={selfUpdate.update} />
    </>
  )
}
