/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { useForm } from "@tanstack/react-form"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Separator } from "@/components/ui/separator"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import {
  useHasPremiumAccess,
  useValidateThemeLicense,
  useDeleteThemeLicense,
  useRefreshThemeLicenses,
  useAllLicenses
} from "@/hooks/useThemeLicense"
import { Key, Trash2, RefreshCw, Sparkles, Copy } from "lucide-react"

export function ThemeLicenseManager() {
  const [showAddLicense, setShowAddLicense] = useState(false)
  const [selectedLicenseKey, setSelectedLicenseKey] = useState<string | null>(null)

  const { hasPremiumAccess, isLoading } = useHasPremiumAccess()
  const { data: licenses } = useAllLicenses()
  const validateLicense = useValidateThemeLicense()
  const deleteLicense = useDeleteThemeLicense()
  const refreshLicenses = useRefreshThemeLicenses()

  const form = useForm({
    defaultValues: {
      licenseKey: "",
    },
    onSubmit: async ({ value }) => {
      await validateLicense.mutateAsync(value.licenseKey)
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
                Manage your theme licenses and premium access
              </CardDescription>
            </div>
            <div className="flex gap-2">
              {licenses && licenses.length > 0 && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => refreshLicenses.mutate()}
                  disabled={refreshLicenses.isPending}
                  className="text-xs sm:text-sm"
                >
                  <RefreshCw className={`h-3 w-3 sm:h-4 sm:w-4 sm:mr-2 ${refreshLicenses.isPending ? "animate-spin" : ""}`} />
                  <span className="hidden sm:inline">Refresh</span>
                </Button>
              )}
              <Button
                size="sm"
                onClick={() => setShowAddLicense(!showAddLicense)}
                className="text-xs sm:text-sm"
              >
                <Key className="h-3 w-3 sm:h-4 sm:w-4 mr-1 sm:mr-2" />
                Add License
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent className="space-y-6">
          {showAddLicense && (
            <div className="space-y-4 p-3 sm:p-4 border rounded-lg bg-muted/30">
              <div>
                <h4 className="font-medium mb-2 text-sm sm:text-base">Add New License</h4>
                <p className="text-xs sm:text-sm text-muted-foreground mb-3 sm:mb-4">
                  Enter your premium theme license key to unlock additional themes.
                </p>
              </div>

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
                      />
                      {field.state.meta.isTouched && field.state.meta.errors[0] && (
                        <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                      )}
                    </div>
                  )}
                </form.Field>

                <div className="flex flex-col sm:flex-row gap-2">
                  <form.Subscribe
                    selector={(state) => [state.canSubmit, state.isSubmitting]}
                  >
                    {([canSubmit, isSubmitting]) => (
                      <Button
                        type="submit"
                        disabled={!canSubmit || isSubmitting || validateLicense.isPending}
                        className="text-xs sm:text-sm"
                      >
                        {isSubmitting || validateLicense.isPending ? "Validating..." : "Activate License"}
                      </Button>
                    )}
                  </form.Subscribe>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setShowAddLicense(false)}
                    className="text-xs sm:text-sm"
                  >
                    Cancel
                  </Button>
                </div>
              </form>
            </div>
          )}


          {/* Active Licenses */}
          {licenses && licenses.length > 0 && (
            <>
              <div>
                <h4 className="font-medium mb-3 sm:mb-4 flex items-center gap-2 text-sm sm:text-base">
                  <Key className="h-3 w-3 sm:h-4 sm:w-4" />
                  Active Licenses
                </h4>
                <div className="space-y-2">
                  {licenses.map((license) => (
                    <div key={license.licenseKey} className="p-3 bg-muted/30 rounded-lg">
                      <div className="flex items-start justify-between gap-2">
                        <div className="flex-1 min-w-0 space-y-1.5">
                          <div className="font-mono text-xs leading-relaxed break-all">
                            {license.licenseKey}
                          </div>
                          <div className="text-xs text-muted-foreground">
                            {license.themeName} • Added {new Date(license.createdAt).toLocaleDateString()}
                          </div>
                        </div>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => handleDeleteLicense(license.licenseKey)}
                          className="text-destructive hover:text-destructive hover:bg-destructive/10 flex-shrink-0 h-8 w-8"
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
              <Separator />
            </>
          )}

          {/* Premium Status */}
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between p-3 sm:p-4 bg-muted/30 rounded-lg gap-3">
            <div className="flex items-center gap-2 sm:gap-3">
              <Sparkles className={hasPremiumAccess ? "h-4 w-4 sm:h-5 sm:w-5 text-primary flex-shrink-0" : "h-4 w-4 sm:h-5 sm:w-5 text-muted-foreground flex-shrink-0"} />
              <div className="min-w-0">
                <p className="font-medium text-sm sm:text-base">
                  {hasPremiumAccess ? "Premium Access Active" : "Unlock Premium Themes"}
                </p>
                <p className="text-xs sm:text-sm text-muted-foreground">
                  {hasPremiumAccess? "You have access to all current and future premium themes": "One-time purchase • $9.99 • All themes"}
                </p>
              </div>
            </div>
            {!hasPremiumAccess && (
              <Button size="sm" asChild className="text-xs sm:text-sm self-stretch sm:self-auto">
                <a href="https://buy.polar.sh/polar_cl_yyXJesVM9pFVfAPIplspbfCukgVgXzXjXIc2N0I8WcL" target="_blank" rel="noopener noreferrer">
                  Get Access
                </a>
              </Button>
            )}
          </div>
        </CardContent>
      </Card>

      {/* Delete License Confirmation Dialog */}
      <Dialog open={!!selectedLicenseKey} onOpenChange={(open) => !open && setSelectedLicenseKey(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Release License Key</DialogTitle>
            <DialogDescription>
              Are you sure you want to release this license? This will remove it from your account and allow it to be used elsewhere.
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
                onClick={() => {
                  navigator.clipboard.writeText(selectedLicenseKey)
                  toast.success("License key copied to clipboard")
                }}
              >
                <Copy className="h-4 w-4 mr-2" />
                Copy License Key
              </Button>

              <div className="text-sm text-muted-foreground">
                <strong>Important:</strong> Make sure to copy this license key before releasing it. Once released, you won't be able to see it again.
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
    </>
  )
}