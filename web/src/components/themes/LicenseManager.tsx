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
import { SUPPORT_CRYPTOCURRENCY_URL } from "@/lib/support-constants"
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

export function LicenseManager({ checkoutStatus, checkoutPaymentStatus, onCheckoutConsumed }: LicenseManagerProps) {
  const { t } = useTranslation("common")
  const tr = useCallback(
    (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never)),
    [t]
  )
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
  const selectedPortalLabel = (selectedLicense?.provider ?? provider) === "polar"
    ? tr("licenseManager.portalLabels.polar")
    : tr("licenseManager.portalLabels.dodo")
  const localizeStatus = useCallback((status: string) => {
    const normalized = status.trim().toLowerCase()
    if (normalized === "active") return tr("licenseManager.status.active")
    if (normalized === "inactive") return tr("licenseManager.status.inactive")
    if (normalized === "expired") return tr("licenseManager.status.expired")
    if (normalized === "revoked") return tr("licenseManager.status.revoked")
    return tr("licenseManager.status.unknown")
  }, [tr])

  // Check if we have an invalid license (exists but not active)
  const hasInvalidLicense = primaryLicense ? primaryLicense.status !== "active" : false
  let accessTitle = tr("licenseManager.access.unlockTitle")
  let accessDescription = tr("licenseManager.access.unlockDescription")
  if (hasPremiumAccess) {
    accessTitle = tr("licenseManager.access.activeTitle")
    accessDescription = tr("licenseManager.access.activeDescription")
  } else if (hasInvalidLicense) {
    accessTitle = tr("licenseManager.access.activationRequiredTitle")
    accessDescription = tr("licenseManager.access.activationRequiredDescription")
  }
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
      toast.success(tr("licenseManager.toasts.paymentCompleted"))
    } else if (normalizedPaymentStatus) {
      toast.error(tr("licenseManager.toasts.paymentNotCompleted"))
    } else {
      toast.success(tr("licenseManager.toasts.returnedFromCheckout"))
    }

    onCheckoutConsumed?.()
  }, [checkoutPaymentStatus, checkoutStatus, onCheckoutConsumed, openAddLicenseDialog])

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
            {tr("licenseManager.title")}
          </CardTitle>
          <CardDescription>{tr("licenseManager.states.loadingLicenses")}</CardDescription>
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
                {tr("licenseManager.title")}
              </CardTitle>
              <CardDescription className="text-xs sm:text-sm mt-1">
                {tr("licenseManager.description")}
              </CardDescription>
            </div>
            <div className="flex gap-2">
              {!hasStoredLicense && (
                <Button
                  size="sm"
                  onClick={() => setShowAddLicense(true)}
                  className="text-xs sm:text-sm"
                >
                  <Key className="h-3 w-3 sm:h-4 sm:w-4 mr-1 sm:mr-2" />
                  {tr("licenseManager.actions.addLicense")}
                </Button>
              )}
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {/* Premium License Status */}
          <div className="p-4 bg-muted/30 rounded-lg">
            <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
              <div className="flex items-start gap-3 flex-1">
                <Sparkles className={hasPremiumAccess ? "h-5 w-5 text-primary flex-shrink-0 mt-0.5" : "h-5 w-5 text-muted-foreground flex-shrink-0 mt-0.5"} />
                <div className="min-w-0 space-y-1 flex-1">
                  <p className="font-medium text-base">{accessTitle}</p>
                  <p className="text-sm text-muted-foreground">{accessDescription}</p>
                  {!hasPremiumAccess && !hasInvalidLicense && (
                    <p className="text-xs text-muted-foreground">
                      {tr("licenseManager.help.buyAndRecoverPrefix")}{" "}
                      <a
                        href={DODO_PORTAL_URL}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-primary underline hover:no-underline"
                      >
                        {tr("licenseManager.portalLabels.dodo")}
                      </a>
                      .
                    </p>
                  )}

                  {/* License Key Details - Show for both active and invalid licenses */}
                  {primaryLicense && (
                    <div className="mt-3 pt-3 border-t border-border/50 space-y-2">
                      <div className="font-mono text-xs break-all text-muted-foreground">
                        {maskLicenseKey(primaryLicense.licenseKey)}
                      </div>
                      <div className="text-xs text-muted-foreground">
                        {tr("licenseManager.labels.licenseMeta", {
                          productName: primaryLicense.productName,
                          status: localizeStatus(primaryLicense.status),
                          date: formatDate(new Date(primaryLicense.createdAt)),
                        })}
                      </div>
                      {hasInvalidLicense && (
                        <div className="space-y-2">
                          <div className="text-xs text-amber-600 dark:text-amber-500 mt-2 flex items-start gap-1">
                            <AlertTriangle className="h-3 w-3 flex-shrink-0 mt-0.5" />
                            {provider === "polar" ? (
                              <span>
                                {tr("licenseManager.help.polarInactivePrefix")}{" "}
                                <a
                                  href={portalUrl}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="underline hover:no-underline inline-flex items-center gap-0.5"
                                >
                                  {portalUrl.replace("https://", "")}
                                  <ExternalLink className="h-2.5 w-2.5" />
                                </a>
                                .
                              </span>
                            ) : (
                              <span>{tr("licenseManager.help.dodoInactive")}</span>
                            )}
                          </div>
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => {
                              // Re-attempt activation with the existing license key
                              if (primaryLicense) {
                                activateLicense.mutate(primaryLicense.licenseKey)
                              }
                            }}
                            disabled={activateLicense.isPending}
                            className="h-7 text-xs"
                          >
                            <RefreshCw className={`h-3 w-3 mr-1 ${activateLicense.isPending ? "animate-spin" : ""}`} />
                            {activateLicense.isPending
                              ? tr("licenseManager.actions.activating")
                              : tr("licenseManager.actions.reactivateLicense")}
                          </Button>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              </div>

              <div className="flex gap-2 flex-shrink-0 flex-wrap sm:flex-nowrap">
                {primaryLicense && (
                  <>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleDeleteLicense(primaryLicense.licenseKey)}
                      className="text-destructive hover:text-destructive hover:bg-destructive/10"
                    >
                      <Trash2 className="h-4 w-4 mr-1" />
                      {tr("licenseManager.actions.remove")}
                    </Button>
                  </>
                )}
                {!hasPremiumAccess && !hasInvalidLicense && (
                  <Button size="sm" onClick={() => setShowPaymentDialog(true)}>
                    <Heart className="h-3 w-3 sm:h-4 sm:w-4" />
                    <Bitcoin className="h-3 w-3 sm:h-4 sm:w-4 -ml-1 mr-1 sm:mr-2" />
                    {tr("licenseManager.actions.getPremium")}
                  </Button>
                )}
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Delete License Confirmation Dialog */}
      <Dialog open={!!selectedLicenseKey} onOpenChange={(open) => !open && setSelectedLicenseKey(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{tr("licenseManager.deleteDialog.title")}</DialogTitle>
            <DialogDescription>
              {tr("licenseManager.deleteDialog.description")}
            </DialogDescription>
          </DialogHeader>

          {selectedLicenseKey && (
            <div className="my-4 space-y-3">
              <div>
                <Label className="text-sm font-medium">{tr("licenseManager.deleteDialog.licenseKeyLabel")}</Label>
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
                    toast.success(tr("licenseManager.toasts.licenseCopied"))
                  } catch {
                    toast.error(tr("licenseManager.toasts.failedCopy"))
                  }
                }}
              >
                <Copy className="h-4 w-4 mr-2" />
                {tr("licenseManager.actions.copyLicenseKey")}
              </Button>

              <div className="text-sm text-muted-foreground">
                {tr("licenseManager.deleteDialog.recoverPrefix")}{" "}
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
              {tr("licenseManager.actions.cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={confirmDeleteLicense}
              disabled={deleteLicense.isPending}
            >
              {deleteLicense.isPending
                ? tr("licenseManager.actions.removing")
                : tr("licenseManager.actions.remove")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Add License Dialog */}
      <Dialog open={showAddLicense} onOpenChange={setShowAddLicense}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{tr("licenseManager.addDialog.title")}</DialogTitle>
            <DialogDescription>
              {tr("licenseManager.addDialog.description")}
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
                  !value ? tr("licenseManager.validation.licenseKeyRequired") : undefined,
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="licenseKey">{tr("licenseManager.addDialog.licenseKeyLabel")}</Label>
                  <Input
                    id="licenseKey"
                    placeholder={tr("licenseManager.addDialog.licenseKeyPlaceholder")}
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
                  {tr("licenseManager.actions.recoverKey")}
                </a>
              </Button>
              <a
                href={POLAR_PORTAL_URL}
                target="_blank"
                rel="noopener noreferrer"
                className="text-xs text-muted-foreground hover:underline sm:mr-auto"
              >
                {tr("licenseManager.actions.legacyPolarPortal")}
              </a>

              <div className="flex gap-2 w-full sm:w-auto">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setShowAddLicense(false)}
                  className="flex-1 sm:flex-none"
                >
                  {tr("licenseManager.actions.cancel")}
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
                      {isSubmitting || activateLicense.isPending
                        ? tr("licenseManager.actions.validating")
                        : tr("licenseManager.actions.activateLicense")}
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
              {tr("licenseManager.paymentDialog.title")}
            </DialogTitle>
            <DialogDescription>
              {tr("licenseManager.access.unlockDescription")}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            {/* Step 1: Checkout */}
            <div className="rounded-lg border bg-background p-4 space-y-3">
              <div className="flex items-center gap-2">
                <div className="flex items-center justify-center h-6 w-6 rounded-full bg-primary text-primary-foreground text-xs font-medium">1</div>
                <p className="text-sm font-semibold">{tr("licenseManager.paymentDialog.steps.choosePaymentMethod")}</p>
              </div>
              <ul className="pl-8 space-y-4">
                <li className="space-y-2">
                  <p className="inline-flex items-center gap-2 text-sm font-medium">
                    {tr("licenseManager.paymentDialog.cardMethodsTitle")}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {tr("licenseManager.paymentDialog.cardMethodsDescription")}
                  </p>
                  <Button size="sm" variant="outline" asChild>
                    <a href={checkoutUrl}>
                      <ExternalLink className="h-4 w-4 mr-2" />
                      {tr("licenseManager.actions.openDodoCheckout")}
                    </a>
                  </Button>
                </li>

                <li className="space-y-2">
                  <p className="inline-flex items-center gap-1 text-sm font-medium">
                    {tr("licenseManager.paymentDialog.cryptoLabel")}
                    <Bitcoin className="h-4 w-4 text-orange-500" />
                  </p>
                  <p className="text-xs font-medium text-muted-foreground">
                    {tr("licenseManager.paymentDialog.cryptoDescription")}
                  </p>
                  <ol className="space-y-1 text-xs text-muted-foreground list-decimal pl-5">
                    <li>
                      {tr("licenseManager.paymentDialog.cryptoSteps.donatePrefix")}{" "}
                      <a
                        href={SUPPORT_CRYPTOCURRENCY_URL}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1 underline underline-offset-4 hover:text-foreground"
                      >
                        {tr("licenseManager.paymentDialog.cryptoSteps.readme")}
                        <ExternalLink className="h-3 w-3" />
                      </a>
                      .
                    </li>
                    <li>
                      {tr("licenseManager.paymentDialog.cryptoSteps.verifyPrefix")}{" "}
                      <a
                        href="https://crypto.getqui.com"
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1 underline underline-offset-4 hover:text-foreground"
                      >
                        crypto.getqui.com
                        <ExternalLink className="h-3 w-3" />
                      </a>{" "}
                      {tr("licenseManager.paymentDialog.cryptoSteps.verifySuffix")}
                    </li>
                    <li>{tr("licenseManager.paymentDialog.cryptoSteps.applyDiscountCode")}</li>
                    <li>{tr("licenseManager.paymentDialog.cryptoSteps.confirmTotal")}</li>
                  </ol>
                  <Button size="sm" variant="outline" asChild>
                    <a href={checkoutUrl}>
                      <ExternalLink className="h-4 w-4 mr-2" />
                      {tr("licenseManager.actions.openDodoCheckout")}
                    </a>
                  </Button>
                  <p className="text-xs text-muted-foreground">
                    {tr("licenseManager.paymentDialog.cryptoManualXmr")}
                  </p>
                </li>
              </ul>
            </div>

            {/* Step 2: Find license key */}
            <div className="rounded-lg border bg-background p-4 space-y-3">
              <div className="flex items-center gap-2">
                <div className="flex items-center justify-center h-6 w-6 rounded-full bg-primary text-primary-foreground text-xs font-medium">2</div>
                <p className="text-sm font-semibold">{tr("licenseManager.paymentDialog.steps.findLicenseKey")}</p>
              </div>
              <div className="pl-8 space-y-2">
                <p className="text-sm text-muted-foreground">
                  {tr("licenseManager.paymentDialog.findLicenseDescription")}
                </p>
                <Button size="sm" variant="outline" asChild>
                  <a href={DODO_PORTAL_URL} target="_blank" rel="noopener noreferrer">
                    <ExternalLink className="h-4 w-4 mr-2" />
                    {tr("licenseManager.actions.openDodoPortal")}
                  </a>
                </Button>
              </div>
            </div>

            {/* Step 3: Enter License */}
            <div className="rounded-lg border bg-background p-4">
              <div className="flex items-center gap-2">
                <div className="flex items-center justify-center h-6 w-6 rounded-full bg-primary text-primary-foreground text-xs font-medium">3</div>
                <p className="text-sm font-semibold">{tr("licenseManager.paymentDialog.steps.activateLicense")}</p>
              </div>
              <div className="pl-8 mt-2 space-y-2">
                <p className="text-sm text-muted-foreground">
                  {tr("licenseManager.paymentDialog.activateDescription")}
                </p>
                <Button size="sm" variant="outline" onClick={openAddLicenseDialog}>
                  <Key className="h-4 w-4 mr-2" />
                  {tr("licenseManager.actions.addLicense")}
                </Button>
              </div>
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowPaymentDialog(false)}>
              {tr("licenseManager.actions.close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
