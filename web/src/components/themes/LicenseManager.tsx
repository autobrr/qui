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
  const selectedPortalLabel = (selectedLicense?.provider ?? provider) === "polar" ? "Polar portal" : "Dodo portal"

  // Check if we have an invalid license (exists but not active)
  const hasInvalidLicense = primaryLicense ? primaryLicense.status !== "active" : false
  let accessTitle = "Unlock Premium Themes"
  let accessDescription = "Pay what you want (min $4.99) • Lifetime license • All themes"
  if (hasPremiumAccess) {
    accessTitle = "Premium Access Active"
    accessDescription = "You have access to all current and future premium themes"
  } else if (hasInvalidLicense) {
    accessTitle = "License Activation Required"
    accessDescription = "Your license needs to be activated on this machine"
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
      toast.success("Payment completed. Enter your license key to activate premium.")
    } else if (normalizedPaymentStatus) {
      toast.error("Payment was not completed. Try checkout again.")
    } else {
      toast.success("Returned from checkout. Enter your license key if payment succeeded.")
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
            License Management
          </CardTitle>
          <CardDescription>Loading theme licenses...</CardDescription>
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
                License Management
              </CardTitle>
              <CardDescription className="text-xs sm:text-sm mt-1">
                Manage your theme license and premium access
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
                  Add License
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
                      Buy on DodoPayments, then enter your license key here. If you lose the key, recover it via the{" "}
                      <a
                        href={DODO_PORTAL_URL}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-primary underline hover:no-underline"
                      >
                        Dodo portal
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
                        {primaryLicense.productName} • Status: {primaryLicense.status} • Added {formatDate(new Date(primaryLicense.createdAt))}
                      </div>
                      {hasInvalidLicense && (
                        <div className="space-y-2">
                          <div className="text-xs text-amber-600 dark:text-amber-500 mt-2 flex items-start gap-1">
                            <AlertTriangle className="h-3 w-3 flex-shrink-0 mt-0.5" />
                            {provider === "polar" ? (
                              <span>
                                This license is not active on this machine. Click re-activate to use it here. If you hit an activation limit, deactivate it on the other machine where it’s active, or manage activations via{" "}
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
                              <span>This license is not active on this machine. Click re-activate to use it here. If you hit an activation limit, deactivate it on the other machine where it’s currently active.</span>
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
                            {activateLicense.isPending ? "Activating..." : "Re-activate License"}
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
                      Remove
                    </Button>
                  </>
                )}
                {!hasPremiumAccess && !hasInvalidLicense && (
                  <Button size="sm" onClick={() => setShowPaymentDialog(true)}>
                    <Heart className="h-3 w-3 sm:h-4 sm:w-4" />
                    <Bitcoin className="h-3 w-3 sm:h-4 sm:w-4 -ml-1 mr-1 sm:mr-2" />
                    Get Premium
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
            <DialogTitle>Remove license?</DialogTitle>
            <DialogDescription>
              Are you sure you want to remove this license from this machine? This will deactivate it here to free up an activation slot.
            </DialogDescription>
          </DialogHeader>

          {selectedLicenseKey && (
            <div className="my-4 space-y-3">
              <div>
                <Label className="text-sm font-medium">License Key to Remove:</Label>
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
                    toast.success("License key copied to clipboard")
                  } catch {
                    toast.error("Failed to copy to clipboard")
                  }
                }}
              >
                <Copy className="h-4 w-4 mr-2" />
                Copy License Key
              </Button>

              <div className="text-sm text-muted-foreground">
                If needed, you can recover it later from your{" "}
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
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={confirmDeleteLicense}
              disabled={deleteLicense.isPending}
            >
              {deleteLicense.isPending ? "Removing..." : "Remove"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Add License Dialog */}
      <Dialog open={showAddLicense} onOpenChange={setShowAddLicense}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add Premium License</DialogTitle>
            <DialogDescription>
              Enter your premium theme license key to unlock all premium themes.
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
                  !value ? "License key is required" : undefined,
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="licenseKey">License Key</Label>
                  <Input
                    id="licenseKey"
                    placeholder="Enter your premium theme license key"
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
                  Recover key?
                </a>
              </Button>
              <a
                href={POLAR_PORTAL_URL}
                target="_blank"
                rel="noopener noreferrer"
                className="text-xs text-muted-foreground hover:underline sm:mr-auto"
              >
                Legacy Polar portal
              </a>

              <div className="flex gap-2 w-full sm:w-auto">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setShowAddLicense(false)}
                  className="flex-1 sm:flex-none"
                >
                  Cancel
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
                      {isSubmitting || activateLicense.isPending ? "Validating..." : "Activate License"}
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
              Get Premium License
            </DialogTitle>
            <DialogDescription>
              Pay what you want (min $4.99) • Lifetime license • All themes
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            {/* Step 1: Checkout */}
            <div className="rounded-lg border bg-background p-4 space-y-3">
              <div className="flex items-center gap-2">
                <div className="flex items-center justify-center h-6 w-6 rounded-full bg-primary text-primary-foreground text-xs font-medium">1</div>
                <p className="text-sm font-semibold">Complete checkout</p>
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pl-8">
                <a
                  href={checkoutUrl}
                  className="flex items-center gap-3 p-3 rounded-lg border bg-muted/30 hover:bg-muted/50 transition-colors"
                >
                  <Sparkles className="h-5 w-5 text-primary" />
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium">DodoPayments checkout</p>
                    <p className="text-xs text-muted-foreground">Card & local methods, returns to qui after payment</p>
                  </div>
                  <ExternalLink className="h-4 w-4 text-muted-foreground" />
                </a>
                <a
                  href={SUPPORT_CRYPTOCURRENCY_URL}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-3 p-3 rounded-lg border bg-muted/30 hover:bg-muted/50 transition-colors"
                >
                  <Bitcoin className="h-5 w-5 text-orange-500" />
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium">Cryptocurrency</p>
                    <p className="text-xs text-muted-foreground">BTC, ETH, XMR, and more (manual)</p>
                  </div>
                  <ExternalLink className="h-4 w-4 text-muted-foreground" />
                </a>
              </div>
            </div>

            {/* Step 2: Find license key */}
            <div className="rounded-lg border bg-background p-4 space-y-3">
              <div className="flex items-center gap-2">
                <div className="flex items-center justify-center h-6 w-6 rounded-full bg-primary text-primary-foreground text-xs font-medium">2</div>
                <p className="text-sm font-semibold">Find your license key</p>
              </div>
              <div className="pl-8 space-y-2">
                <p className="text-sm text-muted-foreground">
                  Your license key is shown after checkout. You can also recover it later from the Dodo customer portal.
                </p>
                <Button size="sm" variant="outline" asChild>
                  <a href={DODO_PORTAL_URL} target="_blank" rel="noopener noreferrer">
                    <ExternalLink className="h-4 w-4 mr-2" />
                    Open Dodo portal
                  </a>
                </Button>
              </div>
            </div>

            {/* Step 3: Enter License */}
            <div className="rounded-lg border bg-background p-4">
              <div className="flex items-center gap-2">
                <div className="flex items-center justify-center h-6 w-6 rounded-full bg-primary text-primary-foreground text-xs font-medium">3</div>
                <p className="text-sm font-semibold">Activate your license</p>
              </div>
              <div className="pl-8 mt-2 space-y-2">
                <p className="text-sm text-muted-foreground">
                  After checkout returns here, use Add License to activate your key.
                </p>
                <Button size="sm" variant="outline" onClick={openAddLicenseDialog}>
                  <Key className="h-4 w-4 mr-2" />
                  Add License
                </Button>
              </div>
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowPaymentDialog(false)}>
              Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
