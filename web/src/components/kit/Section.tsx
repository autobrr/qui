/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { UseQueryResult } from "@tanstack/react-query"
import { Loader2, RefreshCw } from "lucide-react"
import type { FormEvent, ReactNode } from "react"
import { useTranslation } from "react-i18next"

import { useShownSectionTitle } from "@/components/layout/shown-section"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { FieldHelp } from "@/components/ui/field-help"

type SectionQuery = Pick<UseQueryResult, "isLoading" | "isLoadingError" | "error" | "refetch">

export interface SectionSubmit {
  /** The label of the button, for example "Save" or "Change password". */
  label: string
  /** Runs on the button and on Enter in a text field of the section. */
  onSubmit: () => void
  /** A save runs: the button shows a spinner and takes no second submit. */
  pending?: boolean
  disabled?: boolean
}

interface SectionProps {
  title: ReactNode
  description?: ReactNode
  /** Field help for the whole section, in a tooltip after the description. */
  help?: ReactNode
  /**
   * Controls for the whole section, for example an Add button. Beside the title on a wide screen, a full-width row
   * under the description on a phone. A section with actions and no content is a one-row setting.
   */
  actions?: ReactNode
  /** Makes the section a form, with its submit button at the end of the footer. */
  submit?: SectionSubmit
  /** A note at the bottom of the section, for example a link to a related setting. It sits before the submit button. */
  footer?: ReactNode
  /**
   * The queries that the content needs. While one of them loads for the first time, the section shows a placeholder.
   * When a first load fails, it shows the error and a retry button. A failed refetch keeps the content that loaded.
   * A query that is turned off does not hold the content back, so the children must handle its missing data.
   */
  queries?: readonly SectionQuery[]
  /** The anchor for a link with a hash. */
  id?: string
  children?: ReactNode
}

// A phone gets 16 px of padding in place of 24 px, so a row in the card keeps room for its text.
const PAD = "px-6 max-sm:px-4"

/** One card of a page: the title, the description, the actions, and the content of one topic. */
export function Section({ title, description, help, actions, submit, footer, queries = [], id, children }: SectionProps) {
  // On a phone, the page title can already name this section. The title then stays for screen readers only.
  const repeatsPageTitle = title === useShownSectionTitle()
  const failed = queries.filter((query) => query.isLoadingError)
  const loading = failed.length === 0 && queries.some((query) => query.isLoading)
  const ready = failed.length === 0 && !loading

  const content = (children || queries.length > 0) && (
    <CardContent className={`space-y-6 ${PAD}`}>
      {failed.length > 0 ? <SectionError queries={failed} /> : loading ? <SectionPlaceholder /> : children}
    </CardContent>
  )
  const bottom = (footer || submit) && (
    <CardFooter className={`gap-3 ${PAD} max-sm:flex-col max-sm:items-stretch`}>
      {footer && <div className="flex min-w-0 flex-1 items-center gap-1.5 text-xs text-muted-foreground">{footer}</div>}
      {submit && (
        <Button type="submit" className="ml-auto max-sm:h-11 max-sm:w-full" disabled={!ready || submit.pending || submit.disabled}>
          {submit.pending && <Loader2 className="animate-spin" />}
          {submit.label}
        </Button>
      )}
    </CardFooter>
  )

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault()
    if (submit && ready && !submit.pending && !submit.disabled) submit.onSubmit()
  }

  return (
    <Card id={id} className="scroll-mt-6 max-sm:gap-5 max-sm:py-5">
      <CardHeader className={`${PAD} max-sm:has-data-[slot=card-action]:grid-cols-1`}>
        <CardTitle className={repeatsPageTitle ? "sr-only" : undefined}>{title}</CardTitle>
        {(description || help) && (
          <CardDescription className="flex items-center gap-1.5">
            {description && <span>{description}</span>}
            {help && <FieldHelp>{help}</FieldHelp>}
          </CardDescription>
        )}
        {actions && (
          <CardAction className="flex gap-2 max-sm:col-start-1 max-sm:row-span-1 max-sm:row-start-3 max-sm:mt-2 max-sm:justify-self-stretch max-sm:*:h-10 max-sm:*:flex-1">
            {actions}
          </CardAction>
        )}
      </CardHeader>
      {submit ? (
        // display: contents keeps the gap of the card between the content and the footer.
        <form onSubmit={handleSubmit} className="contents" noValidate>
          {content}
          {bottom}
        </form>
      ) : (
        <>
          {content}
          {bottom}
        </>
      )}
    </Card>
  )
}

function SectionPlaceholder() {
  return (
    <div aria-busy="true" className="space-y-1.5">
      {[0, 1, 2].map((row) => <div key={row} className="h-14 animate-pulse rounded-lg bg-muted/50" />)}
    </div>
  )
}

function SectionError({ queries }: { queries: readonly SectionQuery[] }) {
  const { t } = useTranslation("common")
  const detail = queries.map((query) => query.error).find((error) => error instanceof Error && error.message) as Error | undefined

  return (
    <div role="alert" className="flex flex-col gap-3 rounded-lg border border-destructive/40 bg-destructive/5 p-4 sm:flex-row sm:items-center">
      <div className="min-w-0 flex-1 text-sm">
        <p className="font-medium text-destructive">{t("kit.loadFailed")}</p>
        {detail && <p className="mt-0.5 text-xs text-muted-foreground [overflow-wrap:anywhere]">{detail.message}</p>}
      </div>
      <Button type="button" variant="outline" size="sm" className="max-sm:h-10" onClick={() => queries.forEach((query) => void query.refetch())}>
        <RefreshCw />
        {t("kit.retry")}
      </Button>
    </div>
  )
}
