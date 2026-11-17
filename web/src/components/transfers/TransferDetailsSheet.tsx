/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { formatISOTimestamp } from "@/lib/dateTimeUtils"
import { transfersApi } from "@/lib/transfers-api"
import { useQuery } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"
import { shownSteps, stepKey } from "./transfer-display"
import { TransferHints } from "./TransferHints"
import { useIncognitoDisplay } from "./use-incognito-display"
import { useInstanceName } from "./use-instance-name"

interface TransferDetailsSheetProps {
  /** The Transfer to show, or null while the panel is closed. */
  transferId: number | null
  onClose: () => void
}

/** The Details side panel: the torrents, the steps, and the log of one Transfer or Export run. */
export function TransferDetailsSheet({ transferId, onClose }: TransferDetailsSheetProps) {
  const { t } = useTranslation("transfers")
  const instanceName = useInstanceName()
  const display = useIncognitoDisplay()
  const detail = useQuery({
    queryKey: ["transfers", "detail", transferId],
    queryFn: () => transfersApi.get(transferId!),
    enabled: transferId !== null,
    refetchInterval: query => (query.state.data?.finishedAt ? false : 3000),
  })
  const data = detail.data

  return (
    <Sheet open={transferId !== null} onOpenChange={open => !open && onClose()}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle className="break-all pr-6">
            {data ? t(data.kind === "export" ? "details.exportTitle" : "details.title", { name: display.name(data) }) : t("details.loading")}
          </SheetTitle>
          {data && (
            <SheetDescription>
              {t("row.route", { source: instanceName(data.sourceInstanceId), target: instanceName(data.targetInstanceId) })}
              {" · "}
              {t([`states.${data.state}`, "states.fallback"])}
            </SheetDescription>
          )}
        </SheetHeader>

        <div className="space-y-6 px-4 pb-6 text-sm">
          {detail.isError && <p role="alert" className="text-destructive">{t("details.error", { message: detail.error.message })}</p>}
          {data && (
            <>
              {data.failureReason && (
                <section className="space-y-1">
                  <h3 className="font-medium">{t("details.failure")}</h3>
                  <p className="text-destructive">{display.text(data.failureReason)}</p>
                </section>
              )}
              <TransferHints hints={data.hints} />

              <section className="space-y-1">
                <h3 className="font-medium">{t("details.torrents")}</h3>
                <ul className="space-y-1">
                  {data.members.map(member => (
                    <li key={member.hash}>
                      <div className="break-all">{display.name(member)}</div>
                      <div className="break-all text-xs text-muted-foreground">
                        {t("details.torrentOn", { instance: instanceName(member.targetInstanceId), path: display.savePath(member.hash, member.savePath) })}
                      </div>
                    </li>
                  ))}
                </ul>
              </section>

              <section className="space-y-1">
                <h3 className="font-medium">{t("details.steps")}</h3>
                <ol className="space-y-1">
                  {shownSteps(data.steps).map((step, index) => {
                    const key = stepKey(step.name)
                    return (
                      <li key={index} className="flex gap-3">
                        <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{formatISOTimestamp(step.at)}</span>
                        <span>
                          {t(key ?? "stepFallback")}
                          {step.detail && !display.incognito && <span className="text-muted-foreground"> · {step.detail}</span>}
                        </span>
                      </li>
                    )
                  })}
                </ol>
              </section>

              <section className="space-y-1">
                <h3 className="font-medium">{t("details.log")}</h3>
                {data.log.length === 0 ? (
                  <p className="text-muted-foreground">{t("details.noLog")}</p>
                ) : display.incognito ? (
                  <p className="text-muted-foreground">{t("incognitoHidden")}</p>
                ) : (
                  <ol className="space-y-1 font-mono text-xs">
                    {data.log.map((line, index) => (
                      <li key={index} className="flex gap-3">
                        <span className="shrink-0 tabular-nums text-muted-foreground">{formatISOTimestamp(line.at)}</span>
                        <span className="break-words">{line.message}</span>
                      </li>
                    ))}
                  </ol>
                )}
              </section>
            </>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
