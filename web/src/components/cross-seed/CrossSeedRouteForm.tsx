/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { countRoutedTorrents, RouteProtectionConfirmDialog } from "@/components/cross-seed/RouteProtectionConfirm"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { crossSeedRoutesKey, useCrossSeedRoute, useCrossSeedRoutes } from "@/hooks/useCrossSeedRoutes"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstances } from "@/hooks/useInstances"
import { usePathAutocomplete } from "@/hooks/usePathAutocomplete"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { CrossSeedRouteRow, CrossSeedRouteTestRowResult, Instance } from "@/types"
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, Loader2 } from "lucide-react"
import { useId, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

const testResultKeys: Record<CrossSeedRouteTestRowResult, string> = {
  sees: "crossseed:routeForm.testResult.sees",
  seesFolderNotMarker: "crossseed:routeForm.testResult.seesFolderNotMarker",
  cannotSeeFolder: "crossseed:routeForm.testResult.cannotSeeFolder",
  cannotWrite: "crossseed:routeForm.testResult.cannotWrite",
}

// The Cross-seed instance's form of a path, as the route rules compare it
// (pathcmp.NormalizePath): either separator, "." and ".." resolved, case
// ignored on a drive-letter path.
function crossSeedKey(path: string): string {
  const slashed = path.replace(/\\/g, "/")
  const root = slashed.startsWith("//") ? "//" : slashed.startsWith("/") ? "/" : ""
  const drive = /^[a-zA-Z]:/.test(slashed)
  const parts: string[] = []
  for (const part of slashed.split("/")) {
    if (part === "..") {
      // .. stops at the drive, and at the server and share of a UNC path.
      if (parts.length > (root === "//" ? 2 : drive ? 1 : 0)) parts.pop()
    } else if (part !== "." && part !== "") {
      parts.push(part)
    }
  }
  const key = root + parts.join("/")
  return drive ? key.toLowerCase() : key
}

// The card keeps the base folders in one comma-separated field. Two spellings
// of one folder, such as /data/a and /data/a/, are one base folder.
function splitBaseDirs(configured: string): string[] {
  const dirs = new Map<string, string>()
  for (const dir of configured.split(",").map(dir => dir.trim()).filter(Boolean)) {
    if (!dirs.has(crossSeedKey(dir))) dirs.set(crossSeedKey(dir), dir)
  }
  return [...dirs.values()]
}

function nested(parent: string, child: string): boolean {
  return parent === child || child.startsWith(parent.replace(/\/$/, "") + "/")
}

// The first two rows that map to the same folder or to nested folders on the Cross-seed instance.
function overlappingRows(rows: CrossSeedRouteRow[]): [string, string] | undefined {
  const keys = rows.map(row => crossSeedKey(row.mappedPath || row.baseDir))
  for (let i = 0; i < keys.length; i++) {
    for (let j = i + 1; j < keys.length; j++) {
      if (nested(keys[i], keys[j]) || nested(keys[j], keys[i])) return [rows[i].baseDir, rows[j].baseDir]
    }
  }
}

/**
 * The route form of one instance in the Hardlink / Reflink Mode card: the
 * Cross-seed instance picker, one mapped path for each base folder, Test route,
 * save, and clear (ADR 0035). localWrite is the card's Write plus local mode check.
 */
export function CrossSeedRouteForm({ instance, localWrite }: { instance: Instance; localWrite: boolean }) {
  const { t } = useTranslation("crossseed")
  const queryClient = useQueryClient()
  const { instances = [] } = useInstances()
  const { data: routes = [], isSuccess: routesRead } = useCrossSeedRoutes()
  const saved = routes.find(route => route.sourceInstanceId === instance.id)
  const { data: savedRoute, isLoading: isLoadingRoute, isError: routeReadFailed } = useCrossSeedRoute(instance, saved !== undefined)
  const [draft, setDraft] = useState<{ crossSeedInstanceId?: number; paths: Record<string, string> } | null>(null)
  // A save or clear that takes saved rows away waits for a confirm (#3269). No baseDirs means all rows.
  const [confirm, setConfirm] = useState<{ clear: true; baseDirs?: never } | { clear: false; baseDirs?: string[] } | null>(null)

  const sourceNames = routes
    .filter(route => route.crossSeedInstanceId === instance.id)
    .flatMap(route => instances.find(other => other.id === route.sourceInstanceId)?.name ?? [])
  // A Cross-seed instance needs no filesystem access, so its reason comes first.
  let disabledReason: string | undefined
  if (sourceNames.length > 0) {
    disabledReason = t("routeForm.receivesCrossSeeds", { names: sourceNames.join(", ") })
  } else if (!localWrite) {
    disabledReason = instance.filesystemMode === "remote"
      ? t("automations:queryBuilder.capabilityReasons.remoteInstance")
      : t("automations:queryBuilder.capabilityReasons.localFilesystemAccess")
  } else if (!instance.useHardlinks && !instance.useReflinks) {
    disabledReason = t("routeForm.needsLinkMode")
  }

  // A route save needs the Cross-seed instance online on qBittorrent 5.0 or later,
  // the version that also brings path autocomplete.
  const candidates = instances.filter(other =>
    other.id !== instance.id &&
    other.isActive &&
    other.filesystemMode !== "remote" &&
    !routes.some(route => route.sourceInstanceId === other.id)
  )
  const capabilities = useQueries({
    queries: candidates.map(candidate => ({
      queryKey: ["instance-capabilities", candidate.id],
      queryFn: () => api.getInstanceCapabilities(candidate.id),
      enabled: disabledReason === undefined,
      staleTime: 60_000,
    })),
  })
  const options = candidates.filter((_, i) => capabilities[i].data?.supportsPathAutocomplete)
  const savedCrossSeed = instances.find(other => other.id === saved?.crossSeedInstanceId)
  if (savedCrossSeed && !options.includes(savedCrossSeed)) options.unshift(savedCrossSeed)

  const crossSeedInstanceId = draft?.crossSeedInstanceId ?? saved?.crossSeedInstanceId
  const baseRows: CrossSeedRouteRow[] = savedRoute?.rows ?? splitBaseDirs(instance.hardlinkBaseDir).map(baseDir => ({ baseDir, mappedPath: "" }))
  const formRows = baseRows.map(row => ({ baseDir: row.baseDir, mappedPath: draft?.paths[row.baseDir] ?? row.mappedPath }))
  const rows = formRows.map(row => ({ ...row, mappedPath: row.mappedPath.trim() }))
  const overlap = overlappingRows(rows)

  const testRoute = useMutation({
    mutationFn: () => api.testCrossSeedRoute(instance.id, { crossSeedInstanceId: crossSeedInstanceId!, rows }),
    onError: (error) => toast.error(t("routeForm.testFailed"), { description: error.message }),
  })
  // The mutation stays pending until the route reads refresh, so the form never
  // shows the old route with Save on.
  const onRouteChanged = async () => {
    await queryClient.invalidateQueries({ queryKey: crossSeedRoutesKey })
    setDraft(null)
    testRoute.reset()
  }
  const saveRoute = useMutation({
    mutationFn: () => api.saveCrossSeedRoute(instance.id, { crossSeedInstanceId: crossSeedInstanceId!, rows }),
    onSuccess: () => {
      toast.success(t("routeForm.saved"), { description: instance.name })
      return onRouteChanged()
    },
    onError: (error) => toast.error(t("routeForm.saveFailed"), { description: error.message }),
  })
  const clearRoute = useMutation({
    mutationFn: () => api.clearCrossSeedRoute(instance.id),
    onSuccess: () => {
      toast.success(t("routeForm.cleared"), { description: instance.name })
      return onRouteChanged()
    },
    onError: (error) => toast.error(t("routeForm.clearFailed"), { description: error.message }),
  })
  const handleSave = () => {
    if (!saved) return saveRoute.mutate()
    if (crossSeedInstanceId !== saved.crossSeedInstanceId || !savedRoute) return setConfirm({ clear: false })
    // The read also gives a base folder added after the save an empty row, and the orphan scan protects the torrents under it.
    const changed = savedRoute.rows
      // rows is built from savedRoute.rows, in the same order.
      .filter((row, i) => rows[i].mappedPath !== row.mappedPath)
      .map(row => row.baseDir)
    if (changed.length === 0) return saveRoute.mutate()
    setConfirm({ clear: false, baseDirs: changed })
  }
  const handleClear = () => setConfirm({ clear: true })
  // A null read means another tab cleared the route, so no routed torrent loses protection.
  const confirmRows = savedRoute === null ? [] : savedRoute?.rows.filter(row => !confirm?.baseDirs || confirm.baseDirs.includes(row.baseDir))

  const edit = (next: { crossSeedInstanceId?: number; baseDir?: string; mappedPath?: string }) => {
    testRoute.reset()
    setDraft(prev => ({
      crossSeedInstanceId: next.crossSeedInstanceId ?? prev?.crossSeedInstanceId,
      paths: next.baseDir === undefined ? prev?.paths ?? {} : { ...prev?.paths, [next.baseDir]: next.mappedPath ?? "" },
    }))
  }

  const busy = testRoute.isPending || saveRoute.isPending || clearRoute.isPending
  // Until the route list and the saved route are read, the rows have empty paths, and a save would erase the route.
  const routeRead = routesRead && (saved === undefined || savedRoute !== undefined)
  const canSubmit = disabledReason === undefined && crossSeedInstanceId !== undefined && rows.length > 0 && overlap === undefined && routeRead && !busy
  const testResult = testRoute.data
  const picker = (
    <div className="space-y-2">
      <div className="flex items-center gap-1.5">
        <Label htmlFor={`route-picker-${instance.id}`}>{t("routeForm.crossSeedInstance")}</Label>
        <FieldHelp>{t("routeForm.crossSeedInstanceDescription")}</FieldHelp>
      </div>
      {disabledReason && <p className="text-xs text-muted-foreground">{disabledReason}</p>}
      <Select
        value={crossSeedInstanceId === undefined ? "" : String(crossSeedInstanceId)}
        onValueChange={(value) => edit({ crossSeedInstanceId: Number(value) })}
        disabled={disabledReason !== undefined || busy}
      >
        <SelectTrigger id={`route-picker-${instance.id}`}>
          <SelectValue placeholder={t("routeForm.noCrossSeedInstance")} />
        </SelectTrigger>
        <SelectContent>
          {options.map(option => (
            <SelectItem key={option.id} value={String(option.id)}>{option.name}</SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )

  // Without a saved route, a turned-off picker is all there is to show.
  if (disabledReason && !saved) {
    return (
      <div className="space-y-4">
        <Separator />
        {picker}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <Separator />
      {picker}

      {savedRoute?.brokenRule && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertDescription>{t("routeForm.brokenRule", { reason: savedRoute.brokenRule })}</AlertDescription>
        </Alert>
      )}

      {crossSeedInstanceId !== undefined && (isLoadingRoute ? (
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      ) : (
        <div className="space-y-3">
          <div className="flex items-center gap-1.5">
            <Label>{t("routeForm.mappedPaths")}</Label>
            <FieldHelp>{t("routeForm.mappedPathsDescription")}</FieldHelp>
          </div>
          {formRows.map(row => (
            <RoutePathInput
              key={row.baseDir}
              baseDir={row.baseDir}
              crossSeedInstanceId={crossSeedInstanceId}
              value={row.mappedPath}
              onChange={(mappedPath) => edit({ baseDir: row.baseDir, mappedPath })}
              disabled={disabledReason !== undefined || busy}
            />
          ))}
          {overlap && (
            <p className="text-xs text-destructive">{t("routeForm.overlap", { first: overlap[0], second: overlap[1] })}</p>
          )}

          {testResult?.crossSeedUnavailable ? (
            <div className="text-sm text-destructive">
              <p>{t("routeStatus.unavailable")}</p>
              {/* The backend message starts with the same label. */}
              <p className="text-xs text-muted-foreground">{testResult.crossSeedUnavailable.replace(/^Cross-seed instance unavailable: /, "")}</p>
            </div>
          ) : testResult && (
            <ul className="space-y-1 text-sm">
              {testResult.rows.map(result => (
                <li key={result.baseDir} className={result.result === "sees" ? "text-primary" : "text-destructive"}>
                  {t("routeForm.testResultRow", { baseDir: result.baseDir, listedPath: result.listedPath, result: t(testResultKeys[result.result]) })}
                  {result.result === "seesFolderNotMarker" && (
                    <span className="block text-xs text-muted-foreground">{t("routeForm.markerHint")}</span>
                  )}
                  {result.error && <span className="block text-xs text-muted-foreground">{result.error}</span>}
                </li>
              ))}
            </ul>
          )}
        </div>
      ))}

      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" onClick={() => testRoute.mutate()} disabled={!canSubmit}>
          {testRoute.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {t("routeForm.testRoute")}
        </Button>
        <Button size="sm" onClick={handleSave} disabled={!canSubmit}>
          {saveRoute.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {t("routeForm.saveRoute")}
        </Button>
        {saved && (
          <Button size="sm" variant="destructive" onClick={handleClear} disabled={busy}>
            {clearRoute.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {t("routeForm.clearRoute")}
          </Button>
        )}
      </div>

      <RouteProtectionConfirmDialog
        open={confirm !== null}
        onOpenChange={(open) => { if (!open) setConfirm(null) }}
        onConfirm={() => (confirm?.clear ? clearRoute : saveRoute).mutate()}
        sourceName={instance.name}
        crossSeedName={savedCrossSeed?.name ?? ""}
        // A failed read makes the count unknown, also over an older cached read.
        routedTorrents={routeReadFailed ? null : confirmRows && countRoutedTorrents(confirmRows)}
        clear={confirm?.clear}
      />
    </div>
  )
}

// One base folder row: the path where the Cross-seed instance sees it, with
// folder suggestions from the Cross-seed instance.
function RoutePathInput({ baseDir, crossSeedInstanceId, value, onChange, disabled }: {
  baseDir: string
  crossSeedInstanceId: number
  value: string
  onChange: (value: string) => void
  disabled: boolean
}) {
  const { t } = useTranslation("crossseed")
  // Unique in the page: two Source instances can share a Cross-seed instance and a base folder.
  const id = useId()
  const { data: capabilities } = useInstanceCapabilities(crossSeedInstanceId)
  const suggestsFolders = capabilities?.supportsPathAutocomplete ?? false
  const { suggestions, handleInputChange, handleSelect, handleKeyDown, handleBlur, highlightedIndex, showSuggestions, inputRef, listRef } =
    usePathAutocomplete(onChange, crossSeedInstanceId)

  return (
    <div className="space-y-1">
      <Label htmlFor={id} className="text-xs text-muted-foreground">{baseDir}</Label>
      <Input
        ref={inputRef}
        id={id}
        autoComplete="off"
        spellCheck={false}
        value={value}
        disabled={disabled}
        placeholder={t("routeForm.samePathPlaceholder")}
        onChange={(e) => {
          onChange(e.target.value)
          if (suggestsFolders) handleInputChange(e.target.value)
        }}
        onKeyDown={handleKeyDown}
        onBlur={handleBlur}
      />
      {suggestsFolders && showSuggestions && (
        <div className="relative">
          <div ref={listRef} className="absolute left-0 right-0 z-50 mt-1 rounded-md border bg-popover text-popover-foreground shadow-md">
            <div className="max-h-55 overflow-y-auto py-1">
              {suggestions.map((entry, index) => (
                <button
                  key={entry}
                  type="button"
                  title={entry}
                  className={cn("w-full px-3 py-2 text-sm hover:bg-accent hover:text-accent-foreground", highlightedIndex === index && "bg-accent text-accent-foreground")}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => handleSelect(entry)}
                >
                  <span className="block truncate text-left">{entry}</span>
                </button>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
