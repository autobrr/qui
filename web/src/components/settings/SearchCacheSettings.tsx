/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { RefreshCw } from "lucide-react"
import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Facts } from "@/components/kit/Facts"
import { Field } from "@/components/kit/Field"
import { Section } from "@/components/kit/Section"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { api } from "@/lib/api"
import { cn, formatBytes } from "@/lib/utils"

const STATS_QUERY_KEY = ["torznab", "search-cache", "stats"] as const
const TORZNAB_CACHE_MIN_TTL_MINUTES = 1440

/** The Search cache tab: what the Torznab search cache holds, and how long it keeps results. */
export function TorznabSearchCachePanel() {
  const { t } = useTranslation("settings")
  const { formatDate } = useDateTimeFormatters()
  const statsQuery = useQuery({
    queryKey: STATS_QUERY_KEY,
    queryFn: () => api.getTorznabSearchCacheStats(),
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
  const stats = statsQuery.data

  const formatCacheTimestamp = (value?: string | null) => {
    const parsed = value ? new Date(value) : null
    return parsed && !Number.isNaN(parsed.getTime()) ? formatDate(parsed) : "—"
  }

  return (
    <>
      <Section
        title={t("searchCache.title")}
        description={t("searchCache.description")}
        queries={[statsQuery]}
        actions={
          <Button variant="outline" size="sm" disabled={statsQuery.isFetching} onClick={() => void statsQuery.refetch()}>
            <RefreshCw className={cn(statsQuery.isFetching && "animate-spin")} />
            {t("searchCache.refreshStats")}
          </Button>
        }
      >
        {stats && (
          <Facts
            items={[
              { label: t("searchCache.status"), value: stats.enabled ? t("searchCache.enabled") : t("searchCache.disabled") },
              { label: t("searchCache.entries"), value: stats.entries.toLocaleString() },
              { label: t("searchCache.hitCount"), value: stats.totalHits.toLocaleString() },
              { label: t("searchCache.approxSize"), value: stats.approxSizeBytes > 0 ? formatBytes(stats.approxSizeBytes) : "—" },
              { label: t("searchCache.newestEntry"), value: formatCacheTimestamp(stats.newestCachedAt) },
              { label: t("searchCache.lastUsed"), value: formatCacheTimestamp(stats.lastUsedAt) },
            ]}
          />
        )}
      </Section>

      {stats && <CacheTTLSection ttlMinutes={stats.ttlMinutes} />}
    </>
  )
}

function CacheTTLSection({ ttlMinutes }: { ttlMinutes: number }) {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()
  // null: the field shows the saved value. A refetch of the stats does not replace what the user typed.
  const [draft, setDraft] = useState<string | null>(null)
  const [error, setError] = useState<string>()
  const input = useRef<HTMLInputElement>(null)
  const value = draft ?? String(ttlMinutes)

  const mutation = useMutation({
    mutationFn: (minutes: number) => api.updateTorznabSearchCacheSettings(minutes),
    onSuccess: (updated) => {
      toast.success(t("searchCache.toasts.ttlUpdated", { minutes: updated.ttlMinutes }))
      setDraft(null)
      queryClient.setQueryData(STATS_QUERY_KEY, updated)
      void queryClient.invalidateQueries({ queryKey: ["torznab", "search-cache"] })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : t("searchCache.toasts.ttlUpdateFailed"))
    },
  })

  const reject = (message: string) => {
    setError(message)
    input.current?.focus()
  }

  const submit = () => {
    const parsed = Number(value)
    if (!Number.isFinite(parsed)) {
      reject(t("searchCache.validation.invalidNumber"))
      return
    }
    const minutes = Math.floor(parsed)
    if (minutes < TORZNAB_CACHE_MIN_TTL_MINUTES) {
      reject(t("searchCache.validation.ttlMinimum", { min: TORZNAB_CACHE_MIN_TTL_MINUTES }))
      return
    }
    mutation.mutate(minutes)
  }

  return (
    <Section
      title={t("searchCache.configTitle")}
      description={t("searchCache.configDescription")}
      submit={{ label: t("searchCache.saveTTL"), onSubmit: submit, pending: mutation.isPending }}
    >
      <Field
        label={t("searchCache.cacheTTL")}
        description={t("searchCache.ttlHelper", { min: TORZNAB_CACHE_MIN_TTL_MINUTES })}
        error={error}
      >
        <Input
          ref={input}
          type="number"
          inputMode="numeric"
          min={TORZNAB_CACHE_MIN_TTL_MINUTES}
          value={value}
          disabled={mutation.isPending}
          onChange={(event) => {
            setDraft(event.target.value)
            setError(undefined)
          }}
          className="max-sm:h-10 sm:w-48"
        />
      </Field>
    </Section>
  )
}
