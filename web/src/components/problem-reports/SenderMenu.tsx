/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TFunction } from "i18next"
import { Check, ChevronDown, Loader2, LogOut, VenetianMask } from "lucide-react"
import { useTranslation } from "react-i18next"

import { ProviderIcon } from "@/components/problem-reports/ProviderIcon"
import type { SenderControls, SignInView } from "@/components/problem-reports/sender"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { APIError } from "@/lib/api"
import type { ProblemReportProvider } from "@/types"

const PROVIDERS: ProblemReportProvider[] = ["github", "discord"]

export function SenderMenu({ controls, disabled }: { controls: SenderControls; disabled: boolean }) {
  const { t } = useTranslation("common")
  const { sender, signIn } = controls
  if (!sender) return null

  const waiting = signIn.phase === "waiting"
  const label = waiting
    ? t("problemReports.sender.waiting", { provider: t(`problemReports.sender.providers.${signIn.provider}`) })
    : sender.anonymous ? t("problemReports.sender.anonymous") : sender.name

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" disabled={disabled} className="h-7 min-w-0 shrink gap-1.5 px-2 text-xs text-muted-foreground hover:text-foreground">
          {waiting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          {!waiting && sender.anonymous && <VenetianMask className="h-3.5 w-3.5" />}
          {!waiting && !sender.anonymous && <ProviderIcon provider={sender.provider} className="h-3.5 w-3.5" />}
          <span className="truncate">{label}</span>
          <ChevronDown className="h-3 w-3 opacity-60" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-60">
        <DropdownMenuLabel className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{t("problemReports.sender.label")}</DropdownMenuLabel>
        <DropdownMenuItem
          role="menuitemradio"
          aria-checked={sender.anonymous}
          onSelect={() => {
            if (!sender.anonymous || waiting) controls.signOut()
          }}
        >
          <VenetianMask />
          {t("problemReports.sender.anonymous")}
          {sender.anonymous && <Check className="ml-auto text-foreground" />}
        </DropdownMenuItem>
        {!sender.anonymous && (
          <DropdownMenuItem role="menuitemradio" aria-checked>
            <ProviderIcon provider={sender.provider} />
            <span className="truncate">{sender.name}</span>
            <span className="text-xs text-muted-foreground">{t(`problemReports.sender.providers.${sender.provider}`)}</span>
            <Check className="ml-auto text-foreground" />
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        {sender.anonymous ? PROVIDERS.map((provider) => (
          <DropdownMenuItem key={provider} onSelect={() => controls.startSignIn(provider)}>
            <ProviderIcon provider={provider} />
            {t("problemReports.sender.signInWith", { provider: t(`problemReports.sender.providers.${provider}`) })}
          </DropdownMenuItem>
        )) : (
          <DropdownMenuItem onSelect={controls.signOut}>
            <LogOut />
            {t("problemReports.sender.signOut")}
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function SignInNote({ signIn }: { signIn: SignInView }) {
  const { t } = useTranslation("common")
  const message = signInMessage(t, signIn)
  if (message === undefined) return null
  return <p role="alert" className="text-xs text-destructive">{message}</p>
}

function signInMessage(t: TFunction<"common">, signIn: SignInView): string | undefined {
  switch (signIn.phase) {
    case "refused":
      return signIn.reason === "discord_not_member"
        ? t("problemReports.sender.errors.discordNotMember")
        : t("problemReports.sender.errors.refused", { reason: signIn.reason })
    case "expired":
      return t("problemReports.sender.errors.expired")
    case "popupBlocked":
      return t("problemReports.sender.errors.popupBlocked")
    case "failed":
      return signIn.error instanceof APIError && signIn.error.message === "unavailable"
        ? t("problemReports.sender.errors.unavailable")
        : t("problemReports.sender.errors.startFailed", { message: signIn.error.message })
    default:
      return undefined
  }
}
