/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ConfirmDialog } from "@/components/kit/ConfirmDialog"
import { Editor } from "@/components/kit/Editor"
import { Field } from "@/components/kit/Field"
import { Section } from "@/components/kit/Section"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import {
  useActivateLicense,
  useDeleteLicense,
  useHasPremiumAccess,
  useLicenseDetails
} from "@/hooks/useLicense"
import { themeLicenseCheckoutUrl } from "@/lib/license-checkout"
import { getLicenseErrorKey } from "@/lib/license-errors"
import { QUI_DISCORD_URL, SUPPORT_CRYPTOCURRENCY_URL } from "@/lib/support-constants"
import { copyTextToClipboard } from "@/lib/utils"
import { useForm } from "@tanstack/react-form"
import { AlertTriangle, Bitcoin, Copy, ExternalLink, Heart, Key, RefreshCw, Sparkles, Trash2 } from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { Trans, useTranslation } from "react-i18next"
import { toast } from "sonner"
import { LICENSE_PORTAL_URL, SUPPORTER_PRODUCT_NAME } from "@/lib/dodo-constants"
import { LicenseError, LicensePortalLink } from "./LicenseError"

// Helper function to mask license keys for display
function maskLicenseKey(key: string): string {
  if (key.length <= 8) {
    return "***"
  }
  return key.slice(0, 8) + "-***-***-***-***"
}

type LicenseManagerProps = {
  checkoutStatus?: "success"
  checkoutPaymentStatus?: string
  onCheckoutConsumed?: () => void
}

export function LicenseManager({
  checkoutStatus, checkoutPaymentStatus, onCheckoutConsumed }: LicenseManagerProps) {
  const { t } = useTranslation("settings")
  const [showAddLicense, setShowAddLicense] = useState(false)
  const [showPaymentDialog, setShowPaymentDialog] = useState(false)
  const { formatDate } = useDateTimeFormatters()
  const [selectedLicenseKey, setSelectedLicenseKey] = useState<string | null>(null)

  const { hasPremiumAccess, isLoading } = useHasPremiumAccess()
  const { data: licenses } = useLicenseDetails()
  const activateLicense = useActivateLicense()
  const deleteLicense = useDeleteLicense()
  const { reset: resetActivation, isError: activationFailed } = activateLicense
  // The card and the dialog share one activate mutation. Clear a failed activation when the dialog opens or closes.
  // Never reset a pending one: that enables the activate buttons again, and a second click sends a second activation.
  const setAddLicenseOpen = useCallback((open: boolean) => {
    if (activationFailed) {
      resetActivation()
    }
    setShowAddLicense(open)
  }, [activationFailed, resetActivation])
  // The support dialog shows the supporter key. This card shows the theme license.
  const primaryLicense = licenses?.find((license) => license.productName !== SUPPORTER_PRODUCT_NAME)
  const supporterKey = licenses?.find((license) => license.productName === SUPPORTER_PRODUCT_NAME)
  const hasStoredLicense = Boolean(primaryLicense)

  // An offline license comes back without user action when the license server answers, so it needs no re-activation.
  const isOfflineLicense = primaryLicense?.status === "offline"
  const hasInvalidLicense = primaryLicense ? primaryLicense.status !== "active" && !isOfflineLicense : false
  const showPurchase = !hasPremiumAccess && !hasInvalidLicense && !isOfflineLicense
  let accessTitle = t("themes.license.status.unlockTitle")
  let accessDescription = t("themes.license.status.unlockDescription")
  if (hasPremiumAccess && supporterKey && primaryLicense?.status !== "active") {
    // With no active theme license, the access comes from the supporter key.
    accessTitle = t("themes.license.status.supporterTitle")
    accessDescription = t("themes.license.status.supporterDescription")
  } else if (hasPremiumAccess) {
    accessTitle = t("themes.license.status.activeTitle")
    accessDescription = t("themes.license.status.activeDescription")
  } else if (isOfflineLicense) {
    accessTitle = t("themes.license.status.offlineTitle")
    accessDescription = t("themes.license.status.offlineDescription")
  } else if (hasInvalidLicense) {
    accessTitle = t("themes.license.status.activationRequiredTitle")
    accessDescription = t("themes.license.status.activationRequiredDescription")
  } else if (supporterKey) {
    accessDescription = t("themes.license.status.supportEndedDescription")
  }
  const statusLabels: Record<string, string> = {
    invalid: t("themes.license.details.statusLabels.invalid"),
    offline: t("themes.license.details.statusLabels.offline"),
  }
  const canAddLicense = !hasStoredLicense || hasInvalidLicense
  const checkoutUrl = useMemo(themeLicenseCheckoutUrl, [])
  const openAddLicenseDialog = useCallback(() => {
    setShowPaymentDialog(false)
    setAddLicenseOpen(true)
  }, [setAddLicenseOpen])

  useEffect(() => {
    if (checkoutStatus !== "success") {
      return
    }

    const normalizedPaymentStatus = checkoutPaymentStatus?.toLowerCase()

    if (normalizedPaymentStatus === "succeeded" || normalizedPaymentStatus === "success") {
      openAddLicenseDialog()
      toast.success(t("themes.license.toasts.paymentCompleted"))
    } else if (normalizedPaymentStatus) {
      toast.error(t("themes.license.toasts.paymentNotCompleted"))
    } else {
      toast.success(t("themes.license.toasts.returnedFromCheckout"))
    }

    onCheckoutConsumed?.()
  }, [checkoutPaymentStatus, checkoutStatus, onCheckoutConsumed, openAddLicenseDialog, t])

  const form = useForm({
    defaultValues: {
      licenseKey: "",
    },
    onSubmit: async ({ value }) => {
      await activateLicense.mutateAsync(value.licenseKey)
      form.reset()
      setAddLicenseOpen(false)
    },
  })

  const activationErrorKey = getLicenseErrorKey(activateLicense.error)

  if (isLoading) {
    return (
      // Not a Section query: Section would show a failed license check as a load error, and this tab shows the license then.
      <Section title={t("themes.license.loadingTitle")} description={t("themes.license.loadingDescription")}>
        <div className="animate-pulse space-y-2">
          <div className="h-4 bg-muted rounded w-3/4"></div>
          <div className="h-4 bg-muted rounded w-1/2"></div>
        </div>
      </Section>
    )
  }

  return (
    <>
      <Section
        title={t("themes.license.managementTitle")}
        description={t("themes.license.managementDescription")}
        actions={canAddLicense ? (
          <Button size="sm" onClick={() => setAddLicenseOpen(true)}>
            <Key className="sm:mr-2" />
            {t("themes.license.actions.addLicense")}
          </Button>
        ) : undefined}
      >
        {/* Premium License Status */}
        <div className="rounded-lg bg-muted/30 p-3">
          {/* Status header */}
          <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-3">
            <div className="flex items-start gap-3">
              <Sparkles className={hasPremiumAccess ? "h-5 w-5 text-primary flex-shrink-0 mt-0.5" : "h-5 w-5 text-muted-foreground flex-shrink-0 mt-0.5"} />
              <div className="space-y-1">
                <p className="font-medium text-base">{accessTitle}</p>
                <p className="text-sm text-muted-foreground">{accessDescription}</p>
                {showPurchase && (
                  <p className="text-xs text-muted-foreground">
                    {t("themes.license.portalHelp.prefix")}{" "}
                    <a
                      href={LICENSE_PORTAL_URL}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-primary underline hover:no-underline"
                    >
                      {t("themes.license.providers.licensePortal")}
                    </a>
                    {t("themes.license.portalHelp.suffix")}
                  </p>
                )}
              </div>
            </div>
            <div className="flex gap-2 flex-shrink-0 flex-wrap sm:flex-nowrap">
              {primaryLicense && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setSelectedLicenseKey(primaryLicense.licenseKey)}
                  className="text-destructive hover:text-destructive hover:bg-destructive/10"
                >
                  <Trash2 className="h-4 w-4 mr-1" />
                  {t("themes.license.actions.remove")}
                </Button>
              )}
              {showPurchase && (
                <Button size="sm" onClick={() => setShowPaymentDialog(true)}>
                  <Heart className="h-3 w-3 sm:h-4 sm:w-4" />
                  <Bitcoin className="h-3 w-3 sm:h-4 sm:w-4 -ml-1 mr-1 sm:mr-2" />
                  {t("themes.license.actions.getPremium")}
                </Button>
              )}
            </div>
          </div>

          {/* Discord perk */}
          {hasPremiumAccess && (
            <div className="mt-3 border-t border-border/50 pt-3 animate-in fade-in duration-300 motion-reduce:animate-none">
              <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-muted-foreground">
                {t("themes.license.discord.title")}
              </p>
              <p className="mt-1.5 text-sm leading-6 text-muted-foreground">
                {t("themes.license.discord.prefix")} <span className="font-medium text-foreground">qui-premium</span> {t("themes.license.discord.afterRole")}{" "}
                <a
                  href={QUI_DISCORD_URL}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 font-medium text-primary transition-colors hover:text-primary/80"
                >
                  {t("themes.license.discord.openDiscord")}
                  <span className="sr-only">{t("themes.license.discord.opensInNewTab")}</span>
                </a>
                {t("themes.license.discord.afterLink")}{" "}
                <button
                  type="button"
                  onClick={async () => {
                    try {
                      await copyTextToClipboard("/verify")
                      toast.success(t("themes.license.copiedVerify"))
                    } catch {
                      toast.error(t("themes.license.failedCopyVerify"))
                    }
                  }}
                  className="inline-flex items-center rounded-full border border-border/70 bg-background px-2 py-1 text-[11px] font-semibold tracking-[-0.01em] text-foreground shadow-sm transition-colors hover:bg-muted cursor-pointer"
                  aria-label={t("themes.license.discord.copyVerifyAriaLabel")}
                  title={t("themes.license.discord.copyVerifyTitle")}
                >
                  {t("themes.license.discord.verifyCommand")}
                </button>
                {" "}{t("themes.license.discord.afterVerify")} <span className="font-medium text-foreground">{t("themes.license.discord.channel")}</span>{t("themes.license.discord.suffix")}
              </p>
            </div>
          )}

          {/* License key details */}
          {primaryLicense && (
            <div className="mt-3 border-t border-border/50 pt-3 space-y-2">
              <div className="font-mono text-xs break-all text-muted-foreground">
                {maskLicenseKey(primaryLicense.licenseKey)}
              </div>
              <div className="text-xs text-muted-foreground">
                {primaryLicense.status !== "active"? t("themes.license.details.productStatusAndAdded", {
                  productName: primaryLicense.productName,
                  status: statusLabels[primaryLicense.status] ?? t("themes.license.details.statusLabels.unknown"),
                  date: formatDate(new Date(primaryLicense.createdAt)),
                }): t("themes.license.details.productAdded", {
                  productName: primaryLicense.productName,
                  date: formatDate(new Date(primaryLicense.createdAt)),
                })}
              </div>
              {hasInvalidLicense && (
                <div className="space-y-2">
                  {activateLicense.isError ? (
                    <LicenseError error={activateLicense.error} className="text-xs mt-2" />
                  ) : (
                    <div className="text-xs text-amber-600 dark:text-amber-500 mt-2 flex items-start gap-1">
                      <AlertTriangle className="h-3 w-3 flex-shrink-0 mt-0.5" />
                      <span>
                        <Trans ns="settings" i18nKey="themes.license.invalid.dodo" components={{ portal: <LicensePortalLink /> }} />
                      </span>
                    </div>
                  )}
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      if (primaryLicense) {
                        activateLicense.mutate(primaryLicense.licenseKey)
                      }
                    }}
                    disabled={activateLicense.isPending}
                    className="h-7 text-xs"
                  >
                    <RefreshCw className={`h-3 w-3 mr-1 ${activateLicense.isPending ? "animate-spin" : ""}`} />
                    {activateLicense.isPending ? t("themes.license.actions.activating") : t("themes.license.actions.reactivate")}
                  </Button>
                </div>
              )}
            </div>
          )}
        </div>
      </Section>

      {/* Delete License Confirmation Dialog */}
      <ConfirmDialog
        open={selectedLicenseKey !== null}
        onOpenChange={(open) => !open && setSelectedLicenseKey(null)}
        title={t("themes.license.deleteDialog.title")}
        description={t("themes.license.deleteDialog.description")}
        confirmLabel={t("themes.license.actions.remove")}
        onConfirm={() => {
          if (selectedLicenseKey) return deleteLicense.mutateAsync(selectedLicenseKey)
        }}
      >
        {selectedLicenseKey && (
          <div className="space-y-3">
            <div>
              <Label className="text-sm font-medium">{t("themes.license.deleteDialog.keyLabel")}</Label>
              <div className="mt-2 p-3 bg-muted rounded-lg font-mono text-sm break-all">
                {selectedLicenseKey}
              </div>
            </div>

            <Button
              variant="outline"
              size="sm"
              className="w-full"
              onClick={async () => {
                try {
                  await copyTextToClipboard(selectedLicenseKey)
                  toast.success(t("themes.license.toasts.keyCopied"))
                } catch {
                  toast.error(t("themes.license.toasts.copyFailed"))
                }
              }}
            >
              <Copy className="h-4 w-4 mr-2" />
              {t("themes.license.actions.copyLicenseKey")}
            </Button>

            <div className="text-sm text-muted-foreground">
              {t("themes.license.deleteDialog.recoverPrefix")}{" "}
              <a
                href={LICENSE_PORTAL_URL}
                target="_blank"
                rel="noopener noreferrer"
                className="text-primary underline inline-flex items-center gap-1"
              >
                {t("themes.license.providers.licensePortal")}
                <ExternalLink className="h-3 w-3" />
              </a>
            </div>
          </div>
        )}
      </ConfirmDialog>

      <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting] as const}>
        {([canSubmit, isSubmitting]) => (
          <Editor
            open={showAddLicense}
            onOpenChange={setAddLicenseOpen}
            title={t("themes.license.addDialog.title")}
            description={t("themes.license.addDialog.description")}
            submitLabel={t("themes.license.actions.activate")}
            onSubmit={() => void form.handleSubmit()}
            pending={isSubmitting || activateLicense.isPending}
            submitDisabled={!canSubmit}
            error={activationErrorKey && <Trans ns="settings" i18nKey={activationErrorKey} components={{ portal: <LicensePortalLink /> }} />}
            secondaryActions={
              <Button variant="outline" asChild>
                <a href={LICENSE_PORTAL_URL} target="_blank" rel="noopener noreferrer">
                  {t("themes.license.addDialog.recoverKey")}
                </a>
              </Button>
            }
          >
            <form.Field
              name="licenseKey"
              validators={{
                onChange: ({ value }) =>
                  !value ? t("themes.license.addDialog.validation.required") : undefined,
              }}
            >
              {(field) => (
                <Field label={t("themes.license.addDialog.label")} error={field.state.meta.isTouched ? field.state.meta.errors[0] : undefined}>
                  <Input
                    placeholder={t("themes.license.addDialog.placeholder")}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    autoComplete="off"
                  />
                </Field>
              )}
            </form.Field>
          </Editor>
        )}
      </form.Subscribe>

      <Editor
        open={showPaymentDialog}
        onOpenChange={setShowPaymentDialog}
        size="wide"
        title={
          <span className="flex items-center gap-2">
            <Sparkles className="size-5" />
            {t("themes.license.paymentDialog.title")}
          </span>
        }
        description={t("themes.license.paymentDialog.description")}
      >
        <div className="space-y-4">
          {/* Step 1: Checkout */}
          <div className="rounded-lg border bg-background p-4 space-y-3">
            <div className="flex items-center gap-2">
              <div className="flex items-center justify-center h-6 w-6 rounded-full bg-primary text-primary-foreground text-xs font-medium">1</div>
              <p className="text-sm font-semibold">{t("themes.license.paymentDialog.steps.choosePaymentMethod.title")}</p>
            </div>
            <ul className="pl-8 space-y-4">
              <li className="space-y-2">
                <p className="inline-flex items-center gap-2 text-sm font-medium">
                  {t("themes.license.paymentDialog.steps.choosePaymentMethod.cardTitle")}
                </p>
                <p className="text-xs text-muted-foreground">
                  {t("themes.license.paymentDialog.steps.choosePaymentMethod.cardDescription")}
                </p>
                <Button size="sm" variant="outline" asChild>
                  <a href={checkoutUrl}>
                    <ExternalLink className="h-4 w-4 mr-2" />
                    {t("themes.license.actions.openDodoCheckout")}
                  </a>
                </Button>
              </li>

              <li className="space-y-2">
                <p className="inline-flex items-center gap-1 text-sm font-medium">
                  {t("themes.license.paymentDialog.steps.choosePaymentMethod.cryptoTitle")}
                  <Bitcoin className="h-4 w-4 text-orange-500" />
                </p>
                <p className="text-xs font-medium text-muted-foreground">
                  {t("themes.license.paymentDialog.steps.choosePaymentMethod.cryptoDescription")}
                </p>
                <ol className="space-y-1 text-xs text-muted-foreground list-decimal pl-5">
                  <li>
                    {t("themes.license.paymentDialog.steps.choosePaymentMethod.cryptoSteps.readmePrefix")}{" "}
                    <a
                      href={SUPPORT_CRYPTOCURRENCY_URL}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1 underline underline-offset-4 hover:text-foreground"
                    >
                      README
                      <ExternalLink className="h-3 w-3" />
                    </a>
                    {t("themes.license.paymentDialog.steps.choosePaymentMethod.cryptoSteps.readmeSuffix")}
                  </li>
                  <li>
                    {t("themes.license.paymentDialog.steps.choosePaymentMethod.cryptoSteps.verifyPrefix")}{" "}
                    <a
                      href="https://crypto.getqui.com"
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1 underline underline-offset-4 hover:text-foreground"
                    >
                      crypto.getqui.com
                      <ExternalLink className="h-3 w-3" />
                    </a>{" "}{t("themes.license.paymentDialog.steps.choosePaymentMethod.cryptoSteps.verifySuffix")}
                  </li>
                  <li>{t("themes.license.paymentDialog.steps.choosePaymentMethod.cryptoSteps.applyCode")}</li>
                  <li>{t("themes.license.paymentDialog.steps.choosePaymentMethod.cryptoSteps.completeCheckout")}</li>
                </ol>
                <Button size="sm" variant="outline" asChild>
                  <a href={checkoutUrl}>
                    <ExternalLink className="h-4 w-4 mr-2" />
                    {t("themes.license.actions.openDodoCheckout")}
                  </a>
                </Button>
                <p className="text-xs text-muted-foreground">
                  {t("themes.license.paymentDialog.steps.choosePaymentMethod.xmrHelp")}
                </p>
              </li>
            </ul>
          </div>

          {/* Step 2: Find license key */}
          <div className="rounded-lg border bg-background p-4 space-y-3">
            <div className="flex items-center gap-2">
              <div className="flex items-center justify-center h-6 w-6 rounded-full bg-primary text-primary-foreground text-xs font-medium">2</div>
              <p className="text-sm font-semibold">{t("themes.license.paymentDialog.steps.findLicenseKey.title")}</p>
            </div>
            <div className="pl-8 space-y-2">
              <p className="text-sm text-muted-foreground">
                {t("themes.license.paymentDialog.steps.findLicenseKey.description")}
              </p>
              <Button size="sm" variant="outline" asChild>
                <a href={LICENSE_PORTAL_URL} target="_blank" rel="noopener noreferrer">
                  <ExternalLink className="h-4 w-4 mr-2" />
                  {t("themes.license.actions.openLicensePortal")}
                </a>
              </Button>
            </div>
          </div>

          {/* Step 3: Enter License */}
          <div className="rounded-lg border bg-background p-4">
            <div className="flex items-center gap-2">
              <div className="flex items-center justify-center h-6 w-6 rounded-full bg-primary text-primary-foreground text-xs font-medium">3</div>
              <p className="text-sm font-semibold">{t("themes.license.paymentDialog.steps.activateLicense.title")}</p>
            </div>
            <div className="pl-8 mt-2 space-y-2">
              <p className="text-sm text-muted-foreground">
                {t("themes.license.paymentDialog.steps.activateLicense.description")}
              </p>
              <Button size="sm" variant="outline" onClick={openAddLicenseDialog}>
                <Key className="h-4 w-4 mr-2" />
                {t("themes.license.actions.addLicense")}
              </Button>
            </div>
          </div>
        </div>
      </Editor>
    </>
  )
}
