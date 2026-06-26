/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import {
  useActivateLicense,
  useDeleteLicense,
  useHasPremiumAccess,
  useLicenseDetails
} from "@/hooks/useLicense"
import { withBasePath } from "@/lib/base-url"
import { getLicenseErrorMessage } from "@/lib/license-errors"
import { POLAR_PORTAL_URL } from "@/lib/polar-constants"
import { QUI_DISCORD_URL, SUPPORT_CRYPTOCURRENCY_URL } from "@/lib/support-constants"
import { copyTextToClipboard } from "@/lib/utils"
import { useForm } from "@tanstack/react-form"
import { AlertTriangle, Bitcoin, Copy, ExternalLink, Heart, Key, RefreshCw, Sparkles, Trash2 } from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { DODO_CHECKOUT_URL, DODO_PORTAL_URL } from "@/lib/dodo-constants"

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

function buildCheckoutUrlWithReturn(returnUrl: string): string {
  try {
    const checkoutUrl = new URL(DODO_CHECKOUT_URL)
    checkoutUrl.searchParams.set("redirect_url", returnUrl)
    return checkoutUrl.toString()
  } catch {
    const separator = DODO_CHECKOUT_URL.includes("?") ? "&" : "?"
    return `${DODO_CHECKOUT_URL}${separator}redirect_url=${encodeURIComponent(returnUrl)}`
  }
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
  // const validateLicense = useValidateThemeLicense()
  const deleteLicense = useDeleteLicense()
  const primaryLicense = licenses?.[0]
  const hasStoredLicense = Boolean(primaryLicense)
  const provider = primaryLicense?.provider ?? "dodo"
  const portalUrl = provider === "polar" ? POLAR_PORTAL_URL : DODO_PORTAL_URL
  const selectedLicense = selectedLicenseKey ? licenses?.find((l) => l.licenseKey === selectedLicenseKey) : undefined
  const selectedPortalUrl = (selectedLicense?.provider ?? provider) === "polar" ? POLAR_PORTAL_URL : DODO_PORTAL_URL
  const selectedPortalLabel = (selectedLicense?.provider ?? provider) === "polar"? t("themes.license.providers.polarPortal"): t("themes.license.providers.dodoPortal")

  // Check if we have an invalid license (exists but not active)
  const hasInvalidLicense = primaryLicense ? primaryLicense.status !== "active" : false
  let accessTitle = t("themes.license.status.unlockTitle")
  let accessDescription = t("themes.license.status.unlockDescription")
  if (hasPremiumAccess) {
    accessTitle = t("themes.license.status.activeTitle")
    accessDescription = t("themes.license.status.activeDescription")
  } else if (hasInvalidLicense) {
    accessTitle = t("themes.license.status.activationRequiredTitle")
    accessDescription = t("themes.license.status.activationRequiredDescription")
  }
  const canAddLicense = !hasStoredLicense || hasInvalidLicense
  const checkoutUrl = useMemo(() => {
    const returnPath = withBasePath("settings?tab=themes&checkout=success")
    const returnUrl = new URL(returnPath, window.location.origin).toString()
    return buildCheckoutUrlWithReturn(returnUrl)
  }, [])
  const openAddLicenseDialog = useCallback(() => {
    setShowPaymentDialog(false)
    setShowAddLicense(true)
  }, [])

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
      setShowAddLicense(false)
    },
  })

  const handleDeleteLicense = (licenseKey: string) => {
    setSelectedLicenseKey(licenseKey)
  }

  const confirmDeleteLicense = () => {
    if (selectedLicenseKey) {
      deleteLicense.mutate(selectedLicenseKey, {
        onSuccess: () => {
          setSelectedLicenseKey(null)
        },
      })
    }
  }

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Key className="h-5 w-5" />
            {t("themes.license.loadingTitle")}
          </CardTitle>
          <CardDescription>{t("themes.license.loadingDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="animate-pulse space-y-2">
            <div className="h-4 bg-muted rounded w-3/4"></div>
            <div className="h-4 bg-muted rounded w-1/2"></div>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <>
      <Card>
        <CardHeader>
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
            <div>
              <CardTitle className="flex items-center gap-2 text-base sm:text-lg">
                <Key className="h-4 w-4 sm:h-5 sm:w-5" />
                {t("themes.license.managementTitle")}
              </CardTitle>
              <CardDescription className="text-xs sm:text-sm mt-1">
                {t("themes.license.managementDescription")}
              </CardDescription>
            </div>
            <div className="flex gap-2">
              {canAddLicense && (
                <Button
                  size="sm"
                  onClick={() => setShowAddLicense(true)}
                  className="text-xs sm:text-sm"
                >
                  <Key className="h-3 w-3 sm:h-4 sm:w-4 mr-1 sm:mr-2" />
                  {t("themes.license.actions.addLicense")}
                </Button>
              )}
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {/* Premium License Status */}
          <div className="p-4 bg-muted/30 rounded-lg">
            {/* Status header */}
            <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-3">
              <div className="flex items-start gap-3">
                <Sparkles className={hasPremiumAccess ? "h-5 w-5 text-primary flex-shrink-0 mt-0.5" : "h-5 w-5 text-muted-foreground flex-shrink-0 mt-0.5"} />
                <div className="space-y-1">
                  <p className="font-medium text-base">{accessTitle}</p>
                  <p className="text-sm text-muted-foreground">{accessDescription}</p>
                  {!hasPremiumAccess && !hasInvalidLicense && (
                    <p className="text-xs text-muted-foreground">
                      {t("themes.license.portalHelp.prefix")}{" "}
                      <a
                        href={DODO_PORTAL_URL}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-primary underline hover:no-underline"
                      >
                        {t("themes.license.providers.dodoPortal")}
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
                    onClick={() => handleDeleteLicense(primaryLicense.licenseKey)}
                    className="text-destructive hover:text-destructive hover:bg-destructive/10"
                  >
                    <Trash2 className="h-4 w-4 mr-1" />
                    {t("themes.license.actions.remove")}
                  </Button>
                )}
                {!hasPremiumAccess && !hasInvalidLicense && (
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
              <div className="mt-4 border-t border-border/50 pt-4 animate-in fade-in duration-300 motion-reduce:animate-none">
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
                    status: primaryLicense.status,
                    date: formatDate(new Date(primaryLicense.createdAt)),
                  }): t("themes.license.details.productAdded", {
                    productName: primaryLicense.productName,
                    date: formatDate(new Date(primaryLicense.createdAt)),
                  })}
                </div>
                {hasInvalidLicense && (
                  <div className="space-y-2">
                    <div className="text-xs text-amber-600 dark:text-amber-500 mt-2 flex items-start gap-1">
                      <AlertTriangle className="h-3 w-3 flex-shrink-0 mt-0.5" />
                      {provider === "polar" ? (
                        <span>
                          {t("themes.license.invalid.polarPrefix")}{" "}
                          <a
                            href={portalUrl}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="underline hover:no-underline inline-flex items-center gap-0.5"
                          >
                            {portalUrl.replace("https://", "")}
                            <ExternalLink className="h-2.5 w-2.5" />
                          </a>
                          {t("themes.license.invalid.polarSuffix")}
                        </span>
                      ) : (
                        <span>{t("themes.license.invalid.dodo")}</span>
                      )}
                    </div>
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
        </CardContent>
      </Card>

      {/* Delete License Confirmation Dialog */}
      <Dialog open={!!selectedLicenseKey} onOpenChange={(open) => !open && setSelectedLicenseKey(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("themes.license.deleteDialog.title")}</DialogTitle>
            <DialogDescription>
              {t("themes.license.deleteDialog.description")}
            </DialogDescription>
          </DialogHeader>

          {selectedLicenseKey && (
            <div className="my-4 space-y-3">
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
                  href={selectedPortalUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-primary underline inline-flex items-center gap-1"
                >
                  {selectedPortalLabel}
                  <ExternalLink className="h-3 w-3" />
                </a>
              </div>
            </div>
          )}

          <DialogFooter>
            <Button variant="outline" onClick={() => setSelectedLicenseKey(null)}>
              {t("common:actions.cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={confirmDeleteLicense}
              disabled={deleteLicense.isPending}
            >
              {deleteLicense.isPending ? t("themes.license.actions.removing") : t("themes.license.actions.remove")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Add License Dialog */}
      <Dialog open={showAddLicense} onOpenChange={setShowAddLicense}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("themes.license.addDialog.title")}</DialogTitle>
            <DialogDescription>
              {t("themes.license.addDialog.description")}
            </DialogDescription>
          </DialogHeader>

          <form
            onSubmit={(e) => {
              e.preventDefault()
              form.handleSubmit()
            }}
            className="space-y-4"
          >
            <form.Field
              name="licenseKey"
              validators={{
                onChange: ({ value }) =>
                  !value ? t("themes.license.addDialog.validation.required") : undefined,
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="licenseKey">{t("themes.license.addDialog.label")}</Label>
                  <Input
                    id="licenseKey"
                    placeholder={t("themes.license.addDialog.placeholder")}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    autoComplete="off"
                    data-1p-ignore
                  />
                  {field.state.meta.isTouched && field.state.meta.errors[0] && (
                    <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                  )}
                  {activateLicense.isError && (
                    <p className="text-sm text-destructive">
                      {getLicenseErrorMessage(activateLicense.error)}
                    </p>
                  )}
                </div>
              )}
            </form.Field>

            <DialogFooter className="flex flex-col sm:flex-row sm:items-center gap-3">
              <Button variant="outline" asChild className="sm:mr-auto">
                <a href={DODO_PORTAL_URL} target="_blank" rel="noopener noreferrer">
                  {t("themes.license.addDialog.recoverKey")}
                </a>
              </Button>
              <a
                href={POLAR_PORTAL_URL}
                target="_blank"
                rel="noopener noreferrer"
                className="text-xs text-muted-foreground hover:underline sm:mr-auto"
              >
                {t("themes.license.providers.legacyPolarPortal")}
              </a>

              <div className="flex gap-2 w-full sm:w-auto">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setShowAddLicense(false)}
                  className="flex-1 sm:flex-none"
                >
                  {t("common:actions.cancel")}
                </Button>
                <form.Subscribe
                  selector={(state) => [state.canSubmit, state.isSubmitting]}
                >
                  {([canSubmit, isSubmitting]) => (
                    <Button
                      type="submit"
                      disabled={!canSubmit || isSubmitting || activateLicense.isPending}
                      className="flex-1 sm:flex-none"
                    >
                      {isSubmitting || activateLicense.isPending ? t("themes.license.actions.validating") : t("themes.license.actions.activate")}
                    </Button>
                  )}
                </form.Subscribe>
              </div>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Payment Options Dialog */}
      <Dialog open={showPaymentDialog} onOpenChange={setShowPaymentDialog}>
        <DialogContent className="sm:max-w-2xl max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Sparkles className="h-5 w-5" />
              {t("themes.license.paymentDialog.title")}
            </DialogTitle>
            <DialogDescription>
              {t("themes.license.paymentDialog.description")}
            </DialogDescription>
          </DialogHeader>

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
                  <a href={DODO_PORTAL_URL} target="_blank" rel="noopener noreferrer">
                    <ExternalLink className="h-4 w-4 mr-2" />
                    {t("themes.license.actions.openDodoPortal")}
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

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowPaymentDialog(false)}>
              {t("common:actions.close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
