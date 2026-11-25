/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ConfirmDialog } from "@/components/kit/ConfirmDialog"
import { Section } from "@/components/kit/Section"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TruncatedText } from "@/components/ui/truncated-text"
import { themes, getThemeById, type Theme } from "@/config/themes"
import { useHasPremiumAccess } from "@/hooks/useLicense.ts"
import { useBuiltinThemes } from "@/hooks/useBuiltinThemes"
import { useCustomThemes } from "@/hooks/useCustomThemes"
import { useThemeVisibility } from "@/hooks/useThemeVisibility"
import { useTheme } from "@/hooks/useTheme"
import { getThemeVariation } from "@/utils/theme"
import { buildThemeCatalog, themeGroup, themeMatchesSearch, type ThemeGroup } from "@/lib/theme-catalog"
import { themeThumbnailPalette, themeVariationColors, type ThemeThumbnailPalette } from "@/lib/theme-thumbnail"
import { ThemeLayoutSettings } from "@/components/themes/ThemeLayoutSettings"
import { useThemeTargetHint } from "@/hooks/useThemeTargetHint"
import { ThemeMenuDialog } from "@/components/themes/ThemeMenuDialog"
import { ThemeThumbnail } from "@/components/themes/ThemeThumbnail"
import { CustomThemeEditor, type CustomThemeEditorTarget } from "@/components/themes/CustomThemeEditor"
import { CopyButton } from "@/components/ui/copy-button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { api } from "@/lib/api"
import { CUSTOM_THEME_PREFIX, type CustomThemeFile } from "@/lib/custom-themes"
import { useDefaultThemeVisit } from "@/lib/theme-storage"
import { buildThemePrompt } from "@/lib/theme-prompt"
import { cn, copyTextToClipboard } from "@/lib/utils"
import { refreshCustomThemes } from "@/hooks/useCustomThemes"
import { Lock, Check, AlertTriangle, WifiOff, RefreshCw, Plus, Sparkles, Pencil, Trash2, Info, MoreVertical, Search, ListChecks, ChevronDown, ChevronUp } from "lucide-react"
import { useId, useLayoutEffect, useRef, useState } from "react"
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
  onEdit: (file: CustomThemeFileRef) => void
  onRemove: (file: CustomThemeFileRef) => void
  className?: string
}

function CustomThemeActions({ file, onEdit, onRemove, className }: CustomThemeActionsProps) {
  const { t } = useTranslation("settings")
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className={cn("size-6 shrink-0", className)}
          aria-label={t("themes.custom.actionsFor", { theme: file.label })}
        >
          <MoreVertical />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onClick={() => onEdit(file)}>
          <Pencil />
          {t("themes.custom.edit")}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onClick={() => onRemove(file)}>
          <Trash2 />
          {t("themes.custom.remove")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

interface ThemeTileProps {
  theme: Theme
  palette: ThemeThumbnailPalette
  variations: Array<{ id: string; color: string }>
  isSelected: boolean
  onSelect: () => void
  customFile?: CustomThemeFileRef
  onEdit: (file: CustomThemeFileRef) => void
  onRemove: (file: CustomThemeFileRef) => void
}

function ThemeTile({ theme, palette, variations, isSelected, onSelect, customFile, onEdit, onRemove }: ThemeTileProps) {
  const { t } = useTranslation("settings")
  return (
    <li className="relative">
      <Tooltip disabled={!theme.description}>
        <TooltipTrigger asChild>
          <button
            type="button"
            aria-pressed={isSelected}
            onClick={onSelect}
            className={cn(
              "flex w-full flex-col gap-1.5 rounded-lg border bg-card p-1.5 text-left outline-none transition-colors hover:border-muted-foreground/50 focus-visible:ring-[3px] focus-visible:ring-ring/50",
              isSelected && "border-primary ring-1 ring-primary hover:border-primary"
            )}
          >
            <span className="relative block">
              <ThemeThumbnail palette={palette} className={cn(theme.locked && "opacity-60")} />
              {variations.length > 1 && (
                <span className="pointer-events-none absolute bottom-1.5 left-1.5 flex -space-x-1">
                  {variations.slice(0, 3).map((v) => (
                    <span key={v.id} aria-hidden="true" className="size-2.5 rounded-full ring-2 ring-background/80" style={{ backgroundColor: v.color }} />
                  ))}
                  <span className="sr-only">{t("themes.selector.variationCount", { count: variations.length })}</span>
                </span>
              )}
            </span>
            <span className={cn("flex min-w-0 items-center gap-1 px-0.5 text-xs font-medium", customFile && "pr-6")}>
              <span className="line-clamp-2 break-words">{theme.name}</span>
              {isSelected && <Check aria-hidden="true" className="size-3.5 shrink-0 text-primary" />}
              {theme.locked && (
                <>
                  <Lock aria-hidden="true" className="ml-auto size-3 shrink-0 text-muted-foreground" />
                  <span className="sr-only">{t("themes.selector.needsLicense")}</span>
                </>
              )}
            </span>
          </button>
        </TooltipTrigger>
        <TooltipContent>{theme.description}</TooltipContent>
      </Tooltip>
      {customFile && (
        <CustomThemeActions file={customFile} onEdit={onEdit} onRemove={onRemove} className="absolute right-1 bottom-1 max-sm:size-8 max-sm:-right-0.5 max-sm:-bottom-1" />
      )}
    </li>
  )
}

interface CurrentThemeProps {
  theme: Theme
  palette: ThemeThumbnailPalette
  dark: boolean
  variation: string | null
  onVariationSelect: (variationId: string) => void
}

function CurrentTheme({ theme, palette, dark, variation, onVariationSelect }: CurrentThemeProps) {
  const { t } = useTranslation("settings")
  const hint = useThemeTargetHint()
  const variations = themeVariationColors(theme, dark)
  const titleId = useId()

  return (
    <section aria-labelledby={titleId} className="flex items-center gap-4">
      <ThemeThumbnail palette={palette} className="w-48 shrink-0 shadow-sm max-sm:hidden" />
      <div className="min-w-0 space-y-1.5">
        <p className="text-xs text-muted-foreground">{t("themes.current.label")}</p>
        <h3 id={titleId} className="truncate text-lg font-semibold leading-tight">{theme.name}</h3>
        {theme.description && <p className="text-sm text-muted-foreground sm:line-clamp-2">{theme.description}</p>}
        {variations.length > 0 && (
          <div role="group" aria-label={t("themes.current.colors")} className="flex flex-wrap gap-1 pt-1 max-sm:gap-0">
            {variations.map((v) => (
              <Tooltip key={v.id}>
                <TooltipTrigger asChild>
                  <button
                    type="button"
                    aria-pressed={variation === v.id}
                    aria-label={v.id}
                    onClick={() => onVariationSelect(v.id)}
                    className="grid size-7 place-items-center rounded-full outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 max-sm:size-11"
                  >
                    <span
                      className={cn(
                        "size-6 rounded-full ring-offset-2 ring-offset-card max-sm:size-8",
                        variation === v.id ? "ring-2 ring-foreground" : "ring-1 ring-border"
                      )}
                      style={{ backgroundColor: v.color }}
                    />
                  </button>
                </TooltipTrigger>
                <TooltipContent>{v.id}</TooltipContent>
              </Tooltip>
            ))}
          </div>
        )}
        {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
      </div>
    </section>
  )
}

function useGridColumns(grid: HTMLElement | null): number | null {
  const [columns, setColumns] = useState<number | null>(null)
  useLayoutEffect(() => {
    if (!grid) return
    const measure = () => {
      const tracks = getComputedStyle(grid).gridTemplateColumns.split(" ").filter(track => track.endsWith("px"))
      setColumns(tracks.length > 0 ? tracks.length : null)
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(grid)
    return () => observer.disconnect()
  }, [grid])
  return columns
}

const COLLAPSED_ROWS = 2
const UNMEASURED_COLUMNS = 8

function editorKey(target: CustomThemeEditorTarget): string {
  return target.kind === "edit" ? `edit:${target.id}` : "new"
}

type ThemeFilter = "all" | ThemeGroup

interface ThemeSelectorProps {
  openMenu?: boolean
  onMenuOpened?: () => void
}

export function ThemeSelector({ openMenu = false, onMenuOpened }: ThemeSelectorProps) {
  const { t } = useTranslation("settings")
  const { theme: currentTheme, currentTheme: appliedTheme, dark, setTheme, setVariation } = useTheme()
  const { hasPremiumAccess, isLoading, isError } = useHasPremiumAccess()
  const {
    customThemes,
    errors: customThemeErrors,
    directory: customThemesDirectory,
    isFetching: customThemesFetching,
    isError: customThemesError,
    refetch: refetchCustomThemes,
  } = useCustomThemes()
  const builtins = useBuiltinThemes()
  const [visibility, setThemesShown, replaceThemeVisibility] = useThemeVisibility()
  const defaultThemeVisit = useDefaultThemeVisit()
  const queryClient = useQueryClient()
  const [query, setQuery] = useState("")
  const [filter, setFilter] = useState<ThemeFilter>("all")
  const [editorTarget, setEditorTarget] = useState<CustomThemeEditorTarget | null>(null)
  const openEditor = (next: CustomThemeEditorTarget) =>
    setEditorTarget(current => (current && editorKey(current) === editorKey(next) ? current : next))
  const editorRequest = useRef(0)
  const [removeTarget, setRemoveTarget] = useState<CustomThemeFileRef | null>(null)
  const [expanded, setExpanded] = useState(false)
  const [grid, setGrid] = useState<HTMLUListElement | null>(null)
  const columns = useGridColumns(grid)
  const toggleRef = useRef<HTMLButtonElement>(null)
  const gridId = useId()

  const removeTheme = useMutation({
    mutationFn: (file: CustomThemeFileRef) => api.deleteCustomTheme(file.id),
    onSuccess: async () => {
      setRemoveTarget(null)
      toast.success(t("themes.custom.removed"))
      await refreshCustomThemes(queryClient).catch(() => undefined)
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
  const editTheme = async (file: CustomThemeFileRef) => {
    const request = ++editorRequest.current
    let fresh: CustomThemeFile[]
    try {
      fresh = await refreshCustomThemes(queryClient)
    } catch (error) {
      if (request === editorRequest.current) {
        toast.error(t("themes.custom.editLoadFailed", { message: error instanceof Error ? error.message : String(error) }))
      }
      return
    }
    if (request !== editorRequest.current) return
    const current = fresh.find(theme => theme.id === file.id)
    openEditor({ kind: "edit", id: file.id, filename: current?.filename ?? `${file.id}.css`, css: current?.css ?? file.css })
  }
  const newTheme = () => {
    editorRequest.current++
    openEditor({ kind: "new" })
  }

  const isThemeUnlocked = (themeId: string) => !getThemeById(themeId)?.locked

  const premiumThemes = themes.filter(theme => theme.isPremium)
  const themeCatalog = buildThemeCatalog(themes, customThemes)
  const lockedCount = themeCatalog.filter(theme => theme.locked).length
  const paletteOf = (theme: Theme) => themeThumbnailPalette(theme, dark, getThemeVariation(theme.id))
  const activeTheme = getThemeById(currentTheme) ?? appliedTheme

  const searching = query.trim() !== ""
  const matches = themeCatalog.filter(theme => themeMatchesSearch(theme, query))
  const countOf = (group: ThemeGroup) => matches.filter(theme => themeGroup(theme) === group).length
  const filters: ThemeFilter[] = customThemes.length > 0 ? ["all", "free", "licensed", "custom"] : ["all", "free", "licensed"]
  const activeFilter = filters.includes(filter) ? filter : "all"
  const shownThemes = activeFilter === "all" ? matches : matches.filter(theme => themeGroup(theme) === activeFilter)
  const collapsedCount = (columns ?? UNMEASURED_COLUMNS) * COLLAPSED_ROWS
  const canCollapse = !searching && shownThemes.length > collapsedCount
  const visibleThemes = canCollapse && !expanded ? shownThemes.slice(0, collapsedCount) : shownThemes
  const filterLabels: Record<ThemeFilter, string> = {
    all: t("themes.groups.all"),
    free: t("themes.groups.free"),
    licensed: t("themes.groups.licensed"),
    custom: t("themes.groups.custom"),
  }

  const toggleExpanded = () => {
    setExpanded(!expanded)
    if (expanded) requestAnimationFrame(() => toggleRef.current?.scrollIntoView({ block: "nearest" }))
  }

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
      <Section title={t("themes.selector.title")} description={t("themes.selector.loadingDescription")}>
        <div className="animate-pulse space-y-4">
          <div className="flex items-center gap-4">
            <div className="aspect-[16/10] w-24 shrink-0 rounded-md bg-muted sm:w-48" />
            <div className="w-full max-w-64 space-y-2">
              <div className="h-3 w-1/4 rounded bg-muted" />
              <div className="h-5 w-1/2 rounded bg-muted" />
              <div className="h-3 w-full rounded bg-muted" />
            </div>
          </div>
          <div className="grid grid-cols-3 gap-2 border-t pt-4 sm:grid-cols-5 sm:gap-3">
            {[...Array(10)].map((_, i) => (
              <div key={i} className="aspect-[16/12] rounded-lg bg-muted max-sm:[&:nth-child(n+7)]:hidden" />
            ))}
          </div>
        </div>
      </Section>
    )
  }

  return (
    <>
      <Section
        title={t("themes.selector.title")}
        description={t("themes.selector.description")}
        actions={
          <ThemeMenuDialog
            trigger={
              <Button variant="outline" size="sm">
                <ListChecks />
                {t("themes.menu.button")}
              </Button>
            }
            catalog={themeCatalog}
            currentThemeId={currentTheme}
            visibility={visibility}
            paletteOf={paletteOf}
            onShownChange={setThemesShown}
            onReplace={replaceThemeVisibility}
            openRequested={openMenu}
            onOpenRequestHandled={onMenuOpened}
          />
        }
      >
        <div className="space-y-4">
          {defaultThemeVisit && (
            <Alert role="status">
              <Info className="h-4 w-4" />
              <AlertDescription>{t("themes.selector.defaultThemeVisit")}</AlertDescription>
            </Alert>
          )}

          <CurrentTheme
            theme={activeTheme}
            palette={paletteOf(activeTheme)}
            dark={dark}
            variation={getThemeVariation(activeTheme.id)}
            onVariationSelect={(variationId) => handleVariationSelect(activeTheme.id, variationId)}
          />

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

          <div className="@container space-y-3 border-t pt-4">
            <div className="flex flex-col gap-2 @3xl:flex-row @3xl:items-center @3xl:justify-between">
              <div className="relative @3xl:w-60">
                <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  type="search"
                  value={query}
                  onChange={(event) => {
                    setQuery(event.target.value)
                    setExpanded(false)
                  }}
                  placeholder={t("themes.selector.search")}
                  aria-label={t("themes.selector.search")}
                  className="pl-8"
                />
              </div>
              <div role="group" aria-label={t("themes.selector.filterLabel")} className="flex flex-wrap gap-1 sm:gap-1.5">
                {filters.map((value) => {
                  const active = activeFilter === value
                  return (
                    <button
                      key={value}
                      type="button"
                      aria-pressed={active}
                      onClick={() => {
                        setFilter(value)
                        setExpanded(false)
                      }}
                      className={cn(
                        "inline-flex h-8 items-center gap-1.5 rounded-full border px-3 text-sm outline-none transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/50 max-sm:h-9 max-sm:gap-1 max-sm:px-2",
                        active
                          ? "border-primary/50 bg-primary/10 font-medium text-foreground"
                          : "text-muted-foreground hover:bg-muted hover:text-foreground"
                      )}
                    >
                      {filterLabels[value]}
                      <span className="text-muted-foreground tabular-nums">{value === "all" ? matches.length : countOf(value)}</span>
                    </button>
                  )
                })}
              </div>
            </div>

            {!hasPremiumAccess && lockedCount > 0 && (activeFilter === "all" || activeFilter === "licensed") && (
              <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-dashed px-3 py-2">
                <p className="flex items-center gap-2 text-sm text-muted-foreground">
                  <Lock className="size-4 shrink-0" />
                  {t("themes.selector.lockedCount", { count: lockedCount })}
                </p>
                <Button variant="outline" size="sm" onClick={scrollToLicenseManager}>
                  {t("themes.custom.unlockCta")}
                </Button>
              </div>
            )}

            <ul
              ref={setGrid}
              id={gridId}
              aria-label={t("themes.selector.title")}
              className="grid grid-cols-[repeat(auto-fill,minmax(6rem,1fr))] gap-2 sm:grid-cols-[repeat(auto-fill,minmax(8.5rem,1fr))] sm:gap-3"
            >
              {visibleThemes.map((theme) => (
                <ThemeTile
                  key={theme.id}
                  theme={theme}
                  palette={paletteOf(theme)}
                  variations={themeVariationColors(theme, dark)}
                  isSelected={currentTheme === theme.id}
                  onSelect={() => handleThemeSelect(theme.id)}
                  customFile={hasPremiumAccess ? customFileOf(theme) : undefined}
                  onEdit={editTheme}
                  onRemove={setRemoveTarget}
                />
              ))}
            </ul>
            {shownThemes.length === 0 && (
              <p className="py-6 text-center text-sm text-muted-foreground">{t("themes.selector.noMatch", { query: query.trim() })}</p>
            )}
            {canCollapse && (
              <Button
                ref={toggleRef}
                variant="outline"
                size="sm"
                className="w-full max-sm:min-h-11"
                aria-expanded={expanded}
                aria-controls={gridId}
                onClick={toggleExpanded}
              >
                {expanded ? <ChevronUp /> : <ChevronDown />}
                {expanded ? t("themes.selector.showFewer") : t("themes.selector.showAll", { count: shownThemes.length })}
              </Button>
            )}
          </div>
        </div>
      </Section>

      <Section id="theme-sync" title={t("themes.layout.title")}>
        <ThemeLayoutSettings />
      </Section>

      <Section
        title={t("themes.custom.title")}
        description={hasPremiumAccess ? t("themes.custom.description") : t("themes.custom.lockedDescription")}
      >
        <div className="space-y-3">
          {!hasPremiumAccess ? (
            <Button variant="outline" size="sm" onClick={scrollToLicenseManager}>
              <Lock className="h-3 w-3" />
              {t("themes.custom.unlockCta")}
            </Button>
          ) : (
            <>
              <div className="flex flex-wrap gap-2">
                <Button size="sm" className="max-sm:min-h-11 max-sm:flex-1" onClick={newTheme}>
                  <Plus className="h-3 w-3" />
                  {t("themes.custom.newTheme")}
                </Button>
                <Button variant="outline" size="sm" className="max-sm:min-h-11 max-sm:flex-1" onClick={() => void copyPrompt()}>
                  <Sparkles className="h-3 w-3" />
                  {t("themes.custom.copyPrompt")}
                </Button>
              </div>

              <div className="flex min-w-0 items-center gap-1.5 rounded-md border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
                {customThemesError ? (
                  <p className="flex min-w-0 items-center gap-1 font-medium text-destructive">
                    <AlertTriangle className="h-3 w-3 shrink-0" />
                    {t("themes.custom.loadError")}
                  </p>
                ) : (
                  <span className="shrink-0">{t("themes.custom.themeCount", { count: customThemes.length })}</span>
                )}
                {customThemesDirectory && (
                  <>
                    <span aria-hidden="true">·</span>
                    <TruncatedText className="min-w-0 font-mono">
                      {customThemesDirectory}
                    </TruncatedText>
                    <CopyButton value={customThemesDirectory} label={t("themes.custom.folderLabel")} className="size-6 shrink-0 max-sm:size-9" />
                  </>
                )}
                <Button
                  variant="ghost"
                  size="xs"
                  className="ml-auto shrink-0 max-sm:h-9"
                  onClick={() => refetchCustomThemes()}
                  disabled={customThemesFetching}
                >
                  <RefreshCw className={customThemesFetching ? "animate-spin" : undefined} />
                  {t("themes.custom.refresh")}
                </Button>
              </div>

              {!customThemesError && !customThemesFetching && customThemes.length === 0 && customThemeErrors.length === 0 && (
                <p className="text-xs text-muted-foreground">{t("themes.custom.noneFound")}</p>
              )}

              {customThemeErrors.length > 0 && (
                <div className="space-y-1">
                  <p className="flex flex-wrap items-center gap-x-1.5 text-xs">
                    <span className="flex items-center gap-1 font-medium text-destructive">
                      <AlertTriangle className="h-3 w-3 shrink-0" />
                      {t("themes.custom.parseErrorsTitle")}
                    </span>
                    <span className="text-muted-foreground">{t("themes.custom.parseErrorsHint")}</span>
                  </p>
                  <ul className="space-y-0.5 text-xs">
                    {customThemeErrors.map((error) => (
                      <li key={error.filename} className="flex min-w-0 items-center justify-between gap-2">
                        <TruncatedText className="min-w-0 font-mono text-foreground">
                          {error.filename}
                        </TruncatedText>
                        <CustomThemeActions file={{ id: error.id, label: error.filename, css: error.css }} onEdit={editTheme} onRemove={setRemoveTarget} className="max-sm:-my-2.5 max-sm:size-11" />
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </>
          )}
        </div>
      </Section>

      {editorTarget && (
        <CustomThemeEditor
          key={editorKey(editorTarget)}
          target={editorTarget}
          onClose={() => setEditorTarget(current => (current === editorTarget ? null : current))}
        />
      )}

      <ConfirmDialog
        open={removeTarget !== null}
        onOpenChange={(open) => !open && setRemoveTarget(null)}
        title={t("themes.custom.removeTitle", { theme: removeTarget?.label ?? "" })}
        description={t("themes.custom.removeDescription")}
        confirmLabel={t("themes.custom.removeConfirm")}
        onConfirm={() => {
          if (removeTarget) return removeTheme.mutateAsync(removeTarget)
        }}
      />
    </>
  )
}
