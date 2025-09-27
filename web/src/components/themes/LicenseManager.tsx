/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
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
import {
  useActivateLicense,
  useDeleteLicense,
  useHasPremiumAccess,
  useLicenseDetails
} from "@/hooks/useLicense"
import { getLicenseErrorMessage } from "@/lib/license-errors"
import { POLAR_CHECKOUT_URL, POLAR_PORTAL_URL } from "@/lib/polar-constants"
import { copyTextToClipboard } from "@/lib/utils"
import { useForm } from "@tanstack/react-form"
import { AlertTriangle, Copy, ExternalLink, Key, RefreshCw, ShoppingCart, Sparkles, Trash2 } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"

// Helper function to mask license keys for display
function maskLicenseKey(key: string): string {
  if (key.length <= 8) {
    return "***"
  }
  return key.slice(0, 8) + "-***-***-***-***"
}

export function LicenseManager() {
  const [showAddLicense, setShowAddLicense] = useState(false)
  const { formatDate } = useDateTimeFormatters()
  const [selectedLicenseKey, setSelectedLicenseKey] = useState<string | null>(null)

  const { hasPremiumAccess, isLoading } = useHasPremiumAccess()
  const { data: licenses } = useLicenseDetails()
  const activateLicense = useActivateLicense()
  // const validateLicense = useValidateThemeLicense()
  const deleteLicense = useDeleteLicense()

  // Check if we have an invalid license (exists but not active)
  const hasInvalidLicense = licenses && licenses.length > 0 && licenses[0].status !== "active"

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
              {(!licenses || licenses.length === 0) && (
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
                  <p className="font-medium text-base">
                    {hasPremiumAccess ? "Premium Access Active" :hasInvalidLicense ? "License Activation Required" :"Unlock Premium Themes"}
                  </p>
                  <p className="text-sm text-muted-foreground">
                    {hasPremiumAccess ? "You have access to all current and future premium themes" :hasInvalidLicense ? "Your license needs to be activated on this machine" :"One-time purchase • $9.99 • All themes"}
                  </p>

                  {/* License Key Details - Show for both active and invalid licenses */}
                  {licenses && licenses.length > 0 && (
                    <div className="mt-3 pt-3 border-t border-border/50 space-y-2">
                      <div className="font-mono text-xs break-all text-muted-foreground">
                        {maskLicenseKey(licenses[0].licenseKey)}
                      </div>
                      <div className="text-xs text-muted-foreground">
                        {licenses[0].productName} • Status: {licenses[0].status} • Added {formatDate(new Date(licenses[0].createdAt))}
                      </div>
                      {hasInvalidLicense && (
                        <div className="space-y-2">
                          <div className="text-xs text-amber-600 dark:text-amber-500 mt-2 flex items-start gap-1">
                            <AlertTriangle className="h-3 w-3 flex-shrink-0 mt-0.5" />
                            <span>License validation failed. This may occur if the license was activated on another machine or if the database was copied. To deactivate on another machine, visit{" "}
                              <a
                                href={POLAR_PORTAL_URL}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="underline hover:no-underline inline-flex items-center gap-0.5"
                              >
                                {POLAR_PORTAL_URL.replace("https://", "")}
                                <ExternalLink className="h-2.5 w-2.5" />
                              </a>
                            </span>
                          </div>
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => {
                              // Re-attempt activation with the existing license key
                              if (licenses && licenses[0]) {
                                activateLicense.mutate(licenses[0].licenseKey)
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

              <div className="flex gap-2 flex-shrink-0">
                {licenses && licenses.length > 0 && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleDeleteLicense(licenses[0].licenseKey)}
                    className="text-destructive hover:text-destructive hover:bg-destructive/10"
                  >
                    <Trash2 className="h-4 w-4 mr-1" />
                    Remove
                  </Button>
                )}
                {!hasPremiumAccess && !hasInvalidLicense && (
                  <Button size="sm" asChild>
                    <a href={POLAR_CHECKOUT_URL} target="_blank" rel="noopener noreferrer">
                      <ShoppingCart className="h-3 w-3 sm:h-4 sm:w-4 mr-1 sm:mr-2" />
                      Buy license
                    </a>
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
            <DialogTitle>Release License Key</DialogTitle>
            <DialogDescription>
              Are you sure you want to remove this license? You will lose access to all premium themes and the license can be used elsewhere.
            </DialogDescription>
          </DialogHeader>

          {selectedLicenseKey && (
            <div className="my-4 space-y-3">
              <div>
                <Label className="text-sm font-medium">License Key to Release:</Label>
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
                  } catch (err) {
                    console.error("Failed to copy license key:", err)
                    toast.error("Failed to copy license key")
                  }
                }}
              >
                <Copy className="h-4 w-4 mr-2" />
                Copy License Key
              </Button>

              <div className="text-sm text-muted-foreground">
                If needed, you can recover it later from your{" "}
                <a
                  href={POLAR_PORTAL_URL}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-primary underline inline-flex items-center gap-1"
                >
                  Polar portal
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
              {deleteLicense.isPending ? "Releasing..." : "Release License"}
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
                <a href={POLAR_PORTAL_URL} target="_blank" rel="noopener noreferrer">
                  Recover key?
                </a>
              </Button>

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
    </>
  )
}
