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
import { useTranslation } from "react-i18next"
import { useUpdateBannerDismissed } from "@/hooks/useUpdateBannerDismissed"

export function UpdateBanner({ iconOnly = false, installInSettings = false }: { iconOnly?: boolean; installInSettings?: boolean }) {
  const { t } = useTranslation("common")
  const [dismissed, dismiss] = useUpdateBannerDismissed()
  const selfUpdate = useSelfUpdate()

  const { data: updateInfo } = useQuery({
    queryKey: ["latest-version"],
    queryFn: () => api.getLatestVersion(),
    refetchInterval: 2 * 60 * 1000,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })

  const bannerButtonClass = "h-6 text-xs border-success/40 text-success hover:bg-success/10"

  return (
    <>
      {!dismissed && updateInfo && (
        <div>
          <div inert={iconOnly} className={collapseRowClassName(iconOnly)}>
            <div className="min-h-0 overflow-hidden">
              <div className={cn(
                "mb-3 rounded-md border border-success/30 bg-success/10 p-3",
                iconOnly && "w-58"
              )}>
                <div className="flex items-start gap-2">
                  <Download className="h-4 w-4 text-success mt-0.5 flex-shrink-0" />
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-success">
                      {t("updateBanner.updateAvailable")}
                    </p>
                    <p className="text-xs text-success mt-1">
                      {t("updateBanner.versionAvailable", { version: updateInfo.tag_name })}
                    </p>
                    <div className="mt-2">
                      {installInSettings ? (
                        <Button asChild variant="outline" size="sm" className={bannerButtonClass}>
                          <Link to="/settings" search={{ tab: "application", modal: "install-update" }}>
                            <Download className="mr-2 h-4 w-4" />
                            {t("settings:application.update.button")}
                          </Link>
                        </Button>
                      ) : (
                        <InstallUpdateButton selfUpdate={selfUpdate} release={updateInfo} className={bannerButtonClass} />
                      )}
                    </div>
                  </div>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="h-4 w-4 text-success hover:text-foreground"
                    onClick={dismiss}
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
                    className="mb-3 flex h-8 items-center rounded-md px-3 text-success -outline-offset-2 hover:bg-sidebar-accent short:h-7"
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
      <RestartOverlay state={selfUpdate.overlay} update={selfUpdate.update} />
    </>
  )
}
