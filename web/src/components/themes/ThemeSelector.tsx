/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { themes, getThemeById, type Theme } from "@/config/themes"
import { useHasPremiumAccess } from "@/hooks/useLicense.ts"
import { useBuiltinThemes } from "@/hooks/useBuiltinThemes"
import { useCustomThemes } from "@/hooks/useCustomThemes"
import { useThemeVisibility } from "@/hooks/useThemeVisibility"
import { useTheme } from "@/hooks/useTheme"
import { getThemeColors, getThemeVariation } from "@/utils/theme"
import { buildThemeCatalog, isThemeShown } from "@/lib/theme-catalog"
import { ThemeLayoutSettings } from "@/components/themes/ThemeLayoutSettings"
import { CustomThemeEditor, type CustomThemeEditorTarget } from "@/components/themes/CustomThemeEditor"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { api } from "@/lib/api"
import { CUSTOM_THEME_PREFIX } from "@/lib/custom-themes"
import { useDefaultThemeVisit } from "@/lib/default-theme-visit"
import { buildThemePrompt } from "@/lib/theme-prompt"
import { copyTextToClipboard } from "@/lib/utils"
import { refreshCustomThemes } from "@/hooks/useCustomThemes"
import { Lock, Check, Palette, AlertTriangle, WifiOff, FolderOpen, RefreshCw, Eye, EyeOff, Plus, Sparkles, Pencil, Trash2, Info } from "lucide-react"
import { useId, useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface CustomThemeFileRef {
  id: string
  label: string
  css: string
}

interface CustomThemeActionsProps {
  file: CustomThemeFileRef
  describedBy?: string
  onEdit: (file: CustomThemeFileRef) => void
  onRemove: (file: CustomThemeFileRef) => void
}

function CustomThemeActions({ file, describedBy, onEdit, onRemove }: CustomThemeActionsProps) {
  const { t } = useTranslation("settings")
  return (
    <div className="flex items-center gap-1">
      <Button
        variant="ghost"
        size="xs"
        className="h-6"
        aria-describedby={describedBy}
        onClick={(e) => {
          e.stopPropagation()
          onEdit(file)
        }}
      >
        <Pencil />
        {t("themes.custom.edit")}
      </Button>
      <Button
        variant="ghost"
        size="xs"
        className="h-6 text-destructive hover:text-destructive"
        aria-describedby={describedBy}
        onClick={(e) => {
          e.stopPropagation()
          onRemove(file)
        }}
      >
        <Trash2 />
        {t("themes.custom.remove")}
      </Button>
    </div>
  )
}

interface ThemeCardProps {
  theme: Theme
  isSelected: boolean
  isLocked: boolean
  isShown: boolean
  onSelect: () => void
  onVariationSelect: (themeId: string, variationId: string) => void
  onShownChange: (shown: boolean) => void
  customFile?: CustomThemeFileRef
  onEdit: (file: CustomThemeFileRef) => void
  onRemove: (file: CustomThemeFileRef) => void
}

function ThemeCard({ theme, isSelected, isLocked, isShown, onSelect, onVariationSelect, onShownChange, customFile, onEdit, onRemove }: ThemeCardProps) {
  const { t } = useTranslation("settings")
  const titleId = useId()
  // Get current variation for theme (validated)
  const variation = getThemeVariation(theme.id)

  // Helper to extract colors from theme
  const colors = getThemeColors(theme)

  return (
    <div role="listitem" className="h-full">
      <Card
        className={`h-full cursor-pointer gap-2 py-3 transition-all duration-200 hover:shadow-md ${
          isSelected ? "ring-2 ring-primary" : ""
        } ${isLocked ? "opacity-60" : ""}`}
        onClick={!isLocked ? onSelect : undefined}
      >
        <CardHeader className="gap-1 px-3">
          <div className="flex items-center justify-between">
            <CardTitle id={titleId} className="flex min-w-0 items-center gap-1 truncate text-sm">
              {theme.name}
              {isSelected && (
                <Check className="h-4 w-4 shrink-0 text-primary" />
              )}
            </CardTitle>
            {isLocked && (
              <Lock className="h-4 w-4 shrink-0 text-muted-foreground" />
            )}
          </div>
          {theme.description && (
            <CardDescription className="line-clamp-1 text-xs">
              {theme.description}
            </CardDescription>
          )}
        </CardHeader>
        <CardContent className="space-y-2 px-3">
          {/* Theme preview colors and variations */}
          <div className="flex items-center justify-between gap-2 flex-wrap">
            {/* Preview colors */}
            <div className="flex gap-1">
              <div
                className="h-3 w-3 rounded-full ring-1 ring-black/10 dark:ring-white/10"
                style={{
                  backgroundColor: colors.primary,
                  backgroundImage: "none",
                  background: colors.primary + " !important",
                }}
              />
              <div
                className="h-3 w-3 rounded-full ring-1 ring-black/10 dark:ring-white/10"
                style={{
                  backgroundColor: colors.secondary,
                  backgroundImage: "none",
                  background: colors.secondary + " !important",
                }}
              />
              <div
                className="h-3 w-3 rounded-full ring-1 ring-black/10 dark:ring-white/10"
                style={{
                  backgroundColor: colors.accent,
                  backgroundImage: "none",
                  background: colors.accent + " !important",
                }}
              />
            </div>

            {/* Variation colors */}
            {colors.variations && colors.variations.length > 0 && (
              <div className="flex gap-1">
                {colors.variations.map((v) => {
                  const selected = variation === v.id
                  return (
                    <button
                      key={v.id}
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation()
                        onVariationSelect(theme.id, v.id)
                      }}
                      className={`h-3 w-3 rounded-full transition-all ${
                        selected ? "ring-2 ring-black dark:ring-white" : "ring-1 ring-black/10 dark:ring-white/10"
                      }`}
                      style={{
                        backgroundColor: v.color,
                        backgroundImage: "none",
                        background: v.color + " !important",
                      }}
                    />
                  )
                })}
              </div>
            )}
          </div>

          {/* Badges */}
          <div className="flex items-center gap-1 sm:gap-2">
            {theme.isCustom ? (
              <Badge variant="secondary" className="px-1.5 text-[10px]">
                <FolderOpen className="mr-0.5 h-2.5 w-2.5" />
                {t("themes.custom.badge")}
              </Badge>
            ) : theme.isPremium ? null : (
              <Badge variant="outline" className="px-1.5 text-[10px]">
                {t("themes.selector.free")}
              </Badge>
            )}

            {isLocked && (
              <Badge variant="destructive" className="px-1.5 text-[10px]">
                <Lock className="mr-0.5 h-2.5 w-2.5" />
                <span className="hidden sm:inline">{t("themes.selector.locked")}</span>
                <span className="sm:hidden">{t("themes.selector.lock")}</span>
              </Badge>
            )}

            {/* Whether the quick theme pickers in the header and the phone footer list this theme */}
            <Button
              variant="ghost"
              size="xs"
              className="ml-auto h-6"
              aria-describedby={titleId}
              onClick={(e) => {
                e.stopPropagation()
                onShownChange(!isShown)
              }}
            >
              {isShown ? <EyeOff /> : <Eye />}
              {t(isShown ? "themes.selector.hideTheme" : "themes.selector.showTheme")}
            </Button>
          </div>

          {customFile && (
            <CustomThemeActions file={customFile} describedBy={titleId} onEdit={onEdit} onRemove={onRemove} />
          )}
        </CardContent>
      </Card>
    </div>
  )
}

export function ThemeSelector() {
  const { t } = useTranslation("settings")
  const { theme: currentTheme, setTheme, setVariation } = useTheme()
  const { hasPremiumAccess, isLoading, isError } = useHasPremiumAccess()
  const {
    customThemes,
    errors: customThemeErrors,
    directory: customThemesDirectory,
    isFetching: customThemesFetching,
    isError: customThemesError,
    refetch: refetchCustomThemes,
  } = useCustomThemes()
  // Subscribe so the picker re-renders when the async theme registry lands.
  const builtins = useBuiltinThemes()
  const [visibility, setThemeShown] = useThemeVisibility()
  const defaultThemeVisit = useDefaultThemeVisit()
  const queryClient = useQueryClient()
  const [editorTarget, setEditorTarget] = useState<CustomThemeEditorTarget | null>(null)
  const [removeTarget, setRemoveTarget] = useState<CustomThemeFileRef | null>(null)

  const removeTheme = useMutation({
    mutationFn: (file: CustomThemeFileRef) => api.deleteCustomTheme(file.id),
    onSuccess: async () => {
      setRemoveTarget(null)
      toast.success(t("themes.custom.removed"))
      await queryClient.invalidateQueries({ queryKey: ["custom-themes"] })
    },
    onError: (error) => {
      toast.error(t("themes.custom.removeFailed", { message: error.message }))
    },
  })

  const copyPrompt = async () => {
    try {
      await copyTextToClipboard(buildThemePrompt())
      toast.success(t("themes.custom.promptCopied"))
    } catch {
      toast.error(t("themes.custom.promptCopyFailed"))
    }
  }

  const customFileOf = (theme: Theme): CustomThemeFileRef | undefined => {
    if (!theme.isCustom || theme.rawCss === undefined) return undefined
    const id = theme.id.slice(CUSTOM_THEME_PREFIX.length)
    return { id, label: theme.name, css: theme.rawCss }
  }
  // Edit reads the file again, because it can have changed by hand on the server. A failed read opens the copy of the picker.
  const editTheme = async (file: CustomThemeFileRef) => {
    const fresh = await refreshCustomThemes(queryClient).catch(() => [])
    setEditorTarget({ kind: "edit", id: file.id, css: fresh.find(theme => theme.id === file.id)?.css ?? file.css })
  }

  // The server is the authority: a premium theme without a license arrives as
  // a locked stub with no CSS, so the locked flag is the gate.
  const isThemeUnlocked = (themeId: string) => !getThemeById(themeId)?.locked

  const premiumThemes = themes.filter(theme => theme.isPremium)
  const themeCatalog = buildThemeCatalog(themes, customThemes)

  const showThemeLockedToast = () => {
    toast.error(t("themes.toasts.premiumRequired"), {
      description: t("themes.toasts.premiumRequiredDescription"),
    })
  }

  const handleThemeSelect = (themeId: string) => {
    if (isThemeUnlocked(themeId)) {
      setTheme(themeId)
    } else {
      showThemeLockedToast()
    }
  }

  const handleVariationSelect = (themeId: string, variationId: string) => {
    if (isThemeUnlocked(themeId)) {
      setTheme(themeId)
      setVariation(variationId)
    } else {
      showThemeLockedToast()
    }
  }

  const scrollToLicenseManager = () => {
    document.getElementById("license-manager")?.scrollIntoView({ behavior: "smooth", block: "start" })
  }

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Palette className="h-5 w-5" />
            {t("themes.selector.title")}
          </CardTitle>
          <CardDescription>{t("themes.selector.loadingDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="animate-pulse space-y-3">
            <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
              {[...Array(6)].map((_, i) => (
                <div key={i} className="h-24 bg-muted rounded"></div>
              ))}
            </div>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card className="gap-4 py-4">
      <CardHeader className="px-4">
        <CardTitle className="flex items-center gap-2">
          <Palette className="h-5 w-5" />
          {t("themes.selector.title")}
        </CardTitle>
        <CardDescription>
          {t("themes.selector.description")}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 px-4">
        {defaultThemeVisit && (
          <p className="flex items-center gap-2 rounded-md border p-3 text-sm">
            <Info className="h-4 w-4 shrink-0 text-muted-foreground" />
            {t("themes.selector.defaultThemeVisit")}
          </p>
        )}

        {isError && !hasPremiumAccess && (
          <div className="flex items-center gap-2 p-3 rounded-md bg-yellow-50 dark:bg-yellow-950/20 border border-yellow-200 dark:border-yellow-800 text-yellow-800 dark:text-yellow-200">
            <WifiOff className="h-4 w-4 flex-shrink-0" />
            <p className="text-sm">
              {t("themes.selector.verificationUnavailable")}
            </p>
          </div>
        )}

        {builtins.isSuccess && premiumThemes.length === 0 && (
          <div className="flex flex-wrap items-center gap-2 rounded-md border border-dashed p-3">
            <Badge variant="outline" className="border-orange-200 bg-orange-50 text-orange-600 dark:border-orange-800 dark:bg-orange-950/20 dark:text-orange-400">
              <AlertTriangle className="mr-1 h-3 w-3" />
              {t("themes.selector.notLoaded")}
            </Badge>
            <p className="text-xs text-muted-foreground">
              {t("themes.selector.notLoadedDescription")}{" "}
              <code className="rounded bg-muted px-1 py-0.5">make build</code>.
            </p>
          </div>
        )}

        <div
          role="list"
          aria-label={t("themes.selector.title")}
          className="grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-3"
        >
          {themeCatalog.map((theme) => (
            <ThemeCard
              key={theme.id}
              theme={theme}
              isSelected={currentTheme === theme.id}
              isLocked={!isThemeUnlocked(theme.id)}
              isShown={isThemeShown(theme, visibility)}
              onSelect={() => handleThemeSelect(theme.id)}
              onVariationSelect={handleVariationSelect}
              onShownChange={(shown) => setThemeShown(theme, shown)}
              customFile={hasPremiumAccess ? customFileOf(theme) : undefined}
              onEdit={editTheme}
              onRemove={setRemoveTarget}
            />
          ))}
        </div>

        <ThemeLayoutSettings />

        {/* Custom Themes (sideloaded CSS, premium feature) */}
        <div className="rounded-md border bg-muted/20 p-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0 space-y-1">
              <h4 className="flex items-center gap-2 text-sm font-medium">
                <FolderOpen className="h-4 w-4 text-muted-foreground" />
                {t("themes.custom.title")}
                <Badge variant="secondary" className="text-[10px]">
                  {t("themes.custom.badge")}
                </Badge>
              </h4>

              {!hasPremiumAccess ? (
                <>
                  <p className="text-sm font-medium">{t("themes.custom.lockedTitle")}</p>
                  <p className="text-xs text-muted-foreground">
                    {t("themes.custom.lockedDescription")}
                  </p>
                </>
              ) : (
                <>
                  {customThemesDirectory && (
                    <code className="block break-all rounded bg-muted px-2 py-1 text-xs">
                      {customThemesDirectory}
                    </code>
                  )}
                  {customThemesError ? (
                    <p className="flex items-center gap-1 text-xs font-medium text-destructive">
                      <AlertTriangle className="h-3 w-3" />
                      {t("themes.custom.loadError")}
                    </p>
                  ) : (
                    <p className="text-xs text-muted-foreground">
                      {customThemes.length === 0 && !customThemesFetching
                        ? t("themes.custom.noneFound")
                        : t("themes.custom.detectedCount", { total: customThemes.length })}
                    </p>
                  )}
                </>
              )}
            </div>

            {!hasPremiumAccess ? (
              <Button variant="outline" size="sm" onClick={scrollToLicenseManager}>
                <Lock className="mr-1 h-3 w-3" />
                {t("themes.custom.unlockCta")}
              </Button>
            ) : (
              <div className="flex flex-wrap gap-2">
                <Button size="sm" onClick={() => setEditorTarget({ kind: "new" })}>
                  <Plus className="mr-1 h-3 w-3" />
                  {t("themes.custom.newTheme")}
                </Button>
                <Button variant="outline" size="sm" onClick={() => void copyPrompt()}>
                  <Sparkles className="mr-1 h-3 w-3" />
                  {t("themes.custom.copyPrompt")}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => refetchCustomThemes()}
                  disabled={customThemesFetching}
                >
                  <RefreshCw className={`mr-1 h-3 w-3 ${customThemesFetching ? "animate-spin" : ""}`} />
                  {t("themes.custom.refresh")}
                </Button>
              </div>
            )}
          </div>

          {hasPremiumAccess && customThemeErrors.length > 0 && (
            <div className="mt-2 space-y-1 border-t pt-2">
              <p className="flex items-center gap-1 text-xs font-medium text-destructive">
                <AlertTriangle className="h-3 w-3" />
                {t("themes.custom.parseErrorsTitle")}
              </p>
              <ul className="space-y-0.5 text-xs text-destructive">
                {customThemeErrors.map((error) => (
                  <li key={error.filename} className="flex flex-wrap items-center justify-between gap-1">
                    <span className="break-all">{error.filename}</span>
                    <CustomThemeActions file={{ id: error.id, label: error.filename, css: error.css }} onEdit={editTheme} onRemove={setRemoveTarget} />
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      </CardContent>

      {editorTarget && (
        <CustomThemeEditor target={editorTarget} onClose={() => setEditorTarget(null)} />
      )}

      <AlertDialog
        open={removeTarget !== null}
        onOpenChange={(open) => {
          if (!open && !removeTheme.isPending) setRemoveTarget(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("themes.custom.removeTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("themes.custom.removeDescription", { theme: removeTarget?.label ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={removeTheme.isPending}>{t("common:actions.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              disabled={removeTheme.isPending}
              onClick={(event) => {
                event.preventDefault()
                if (removeTarget) removeTheme.mutate(removeTarget)
              }}
            >
              {t("themes.custom.removeConfirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}
