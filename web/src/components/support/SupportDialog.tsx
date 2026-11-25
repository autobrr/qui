/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { LicenseError, LicensePortalLink } from "@/components/themes/LicenseError"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Drawer, DrawerClose, DrawerContent, DrawerDescription, DrawerTitle } from "@/components/ui/drawer"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { useActivateLicense, useDeleteLicense, useHasPremiumAccess, useLicenseDetails } from "@/hooks/useLicense"
import { useIsPhone } from "@/hooks/useMediaQuery"
import { isDemo } from "@/lib/demo"
import { DODO_CUSTOMER_PORTAL_URL, DODO_SUPPORTER_PLANS, SUPPORTER_PRODUCT_NAME, THEME_LICENSE_MIN_PRICE } from "@/lib/dodo-constants"
import { themeLicenseCheckoutUrl } from "@/lib/license-checkout"
import { RELEASE_MAP_DAYS } from "@/lib/release-days"
import { cn } from "@/lib/utils"
import { ArrowUpRight, Check, Heart, X } from "lucide-react"
import { useEffect, useId, useState, type FormEvent, type ReactNode } from "react"
import { Trans, useTranslation } from "react-i18next"
import { setSupportOpen, useSupportOpen } from "./open"
import { answerReminder, hideShortcut, turnOffReminder, useSupportReminder } from "./reminder"

type SupportDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

const LICENSE_PLAN = "license"
const LINK = "-my-2 py-2 underline underline-offset-4 hover:text-foreground"
const PHONE_BOTTOM = "pb-[calc(1.25rem+env(safe-area-inset-bottom))]"

function focusContent(event: Event) {
  event.preventDefault()
  if (event.target instanceof HTMLElement) event.target.focus()
}

export function SupportDialogRoot() {
  return <SupportDialog open={useSupportOpen()} onOpenChange={setSupportOpen} />
}

export function SupportDialog({ open, onOpenChange }: SupportDialogProps) {
  const { t } = useTranslation("common")
  const isPhone = useIsPhone()

  if (isPhone) {
    return (
      <Drawer open={open} onOpenChange={onOpenChange}>
        <DrawerContent className="max-h-[94dvh] outline-none" onOpenAutoFocus={focusContent}>
          <DrawerTitle className="sr-only">{t("support.title")}</DrawerTitle>
          <DrawerDescription className="sr-only">{t("support.description")}</DrawerDescription>
          <DrawerClose asChild>
            <button
              type="button"
              aria-label={t("actions.close")}
              className="absolute top-0 right-1 flex h-11 w-11 items-center justify-center rounded-full text-muted-foreground hover:text-foreground"
            >
              <X className="h-5 w-5" />
            </button>
          </DrawerClose>
          <div className="mt-5 min-h-0 flex-1 overflow-y-auto px-5">
            <SupportBody onClose={() => onOpenChange(false)} />
          </div>
        </DrawerContent>
      </Drawer>
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="max-h-[calc(100dvh-2rem)] overflow-y-auto outline-none sm:max-w-lg"
        overlayClassName="backdrop-blur-sm"
        onOpenAutoFocus={focusContent}
      >
        <DialogTitle className="sr-only">{t("support.title")}</DialogTitle>
        <DialogDescription className="sr-only">{t("support.description")}</DialogDescription>
        <SupportBody onClose={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  )
}

function Heading({ children }: { children: ReactNode }) {
  return <h2 className="flex items-center gap-2 pr-8 pb-2 text-lg leading-7 font-semibold tracking-tight sm:text-xl">{children}</h2>
}

function ReleaseMap({ days }: { days: number[] }) {
  const { t } = useTranslation("common")
  const releaseDays = new Set(days)

  return (
    <section className="space-y-2">
      <p className="text-sm leading-6">{t("support.releases", { count: days.length })}</p>
      <div
        className="grid grid-flow-col grid-cols-[repeat(52,minmax(0,1fr))] grid-rows-7 gap-[2px]"
        role="img"
        aria-label={t("support.releaseMap")}
      >
        {Array.from({ length: RELEASE_MAP_DAYS }, (_, day) => (
          <span key={day} className={cn("aspect-square rounded-[1.5px]", releaseDays.has(day) ? "bg-foreground/90" : "bg-foreground/12")} />
        ))}
      </div>
      <p className="flex items-center gap-1.5 text-xs leading-5 text-muted-foreground">
        <span aria-hidden="true" className="inline-block h-2 w-2 shrink-0 rounded-[1.5px] bg-foreground/90" />
        {t("support.releaseDay")}
      </p>
    </section>
  )
}

function SupportBody({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation("common")
  const isPhone = useIsPhone()
  const { hasPremiumAccess, isSupporter } = useHasPremiumAccess()
  const reminder = useSupportReminder()
  const [reminderDays] = useState(reminder.due ? reminder.days : null)
  const { data: licenses } = useLicenseDetails()
  const deleteLicense = useDeleteLicense()
  const [addingKey, setAddingKey] = useState(false)
  const [planId, setPlanId] = useState<string>(DODO_SUPPORTER_PLANS[0].id)

  useEffect(() => {
    if (reminder.due) answerReminder()
  }, [reminder.due])

  const supporterKey = licenses?.find((license) => license.productName === SUPPORTER_PRODUCT_NAME)
  const themeLicense = licenses?.find((license) => license.productName !== SUPPORTER_PRODUCT_NAME)
  const removeKey = supporterKey && (
    <button
      type="button"
      className={LINK}
      disabled={deleteLicense.isPending}
      onClick={() => deleteLicense.mutate(supporterKey.licenseKey)}
    >
      {t("support.removeKey")}
    </button>
  )

  if (isSupporter) {
    return (
      <div className={cn("space-y-4", isPhone && PHONE_BOTTOM)}>
        <Heading>
          <Heart className="h-4 w-4 fill-current text-red-500" />
          {t("support.supporter.title")}
        </Heading>
        <p className="text-sm leading-6 text-muted-foreground">{t("support.supporter.thanks")}</p>
        <p className="text-xs leading-5 text-muted-foreground max-sm:text-[13px]">
          <Trans ns="common" i18nKey="support.supporter.detail" components={{ portal: <LicensePortalLink /> }} />
        </p>
        <Button variant="outline" className="h-11 w-full justify-between px-4 sm:h-10" asChild>
          <a href={DODO_CUSTOMER_PORTAL_URL} target="_blank" rel="noopener noreferrer">
            {t("support.supporter.manage")}
            <ArrowUpRight className="h-3.5 w-3.5" />
          </a>
        </Button>
        <p className="text-xs leading-5 text-muted-foreground max-sm:text-[13px]">{removeKey}</p>
      </div>
    )
  }

  if (addingKey) {
    return (
      <div className={cn("space-y-4", isPhone && PHONE_BOTTOM)}>
        <Heading>{t("support.addKey")}</Heading>
        <SupporterKeyForm onDone={() => setAddingKey(false)} />
      </div>
    )
  }

  const licensed = hasPremiumAccess || Boolean(themeLicense)
  const selectedId = licensed && planId === LICENSE_PLAN ? DODO_SUPPORTER_PLANS[0].id : planId
  const supporterPlan = DODO_SUPPORTER_PLANS.find((plan) => plan.id === selectedId)
  const releaseDays = import.meta.env.VITE_RELEASE_DAYS ?? []

  const row = (id: string, title: string, note: string, tag?: string) => (
    <label
      key={id}
      className={cn(
        "flex min-h-[3.25rem] cursor-pointer items-center gap-3 border-b px-3.5 py-1.5 last:border-b-0 sm:min-h-0 sm:py-2",
        id === selectedId ? "bg-accent/60" : "hover:bg-muted/40"
      )}
    >
      <RadioGroupItem value={id} />
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium tabular-nums">{title}</span>
        <span className="block text-xs leading-5 text-muted-foreground">{note}</span>
      </span>
      {tag && <span className="shrink-0 rounded-full border border-foreground/20 px-2 py-0.5 text-[11px] text-muted-foreground">{tag}</span>}
    </label>
  )

  return (
    <div className="flex flex-col gap-4">
      <div className="space-y-2">
        <Heading>{t("support.heading")}</Heading>
        {reminderDays !== null && (
          <p className="text-xs leading-5 text-muted-foreground">{t("support.reminder.serverDays", { count: reminderDays })}</p>
        )}
        <p className="text-sm leading-5 text-muted-foreground sm:leading-6">{t("support.intro")}</p>
        {licensed && (
          <p className="flex items-start gap-2 text-sm leading-5 sm:leading-6">
            <Check className="mt-0.5 h-4 w-4 shrink-0 text-green-500 sm:mt-1" />
            {t("support.licensed")}
          </p>
        )}
        {supporterKey?.status === "invalid" && (
          <div className="space-y-1.5 rounded-md border px-3 py-2 text-xs leading-5 text-muted-foreground max-sm:text-[13px]">
            <p>
              <Trans ns="common" i18nKey="support.ended" components={{ portal: <LicensePortalLink /> }} />
            </p>
            <p className="flex flex-wrap gap-x-4">
              <a href={DODO_CUSTOMER_PORTAL_URL} target="_blank" rel="noopener noreferrer" className={LINK}>
                {t("support.supporter.manage")}
              </a>
              {removeKey}
            </p>
          </div>
        )}
      </div>

      {releaseDays.length > 0 && <ReleaseMap days={releaseDays} />}

      <RadioGroup value={selectedId} onValueChange={setPlanId} className="gap-0 overflow-hidden rounded-md border">
        {DODO_SUPPORTER_PLANS.map((plan) => row(
          plan.id,
          t("support.plans.yearly", { price: plan.price }),
          t(licensed ? "support.plans.yearlyNote" : "support.plans.yearlyNoteWithLicense", { price: plan.monthlyPrice })
        ))}
        {!licensed && row(
          LICENSE_PLAN,
          t("support.plans.license", { price: THEME_LICENSE_MIN_PRICE }),
          t("support.plans.licenseNote"),
          t("support.plans.onePayment")
        )}
      </RadioGroup>

      <div className={cn("space-y-2", isPhone && "sticky bottom-0 order-last -mx-5 border-t bg-background px-5 pt-3 pb-[calc(0.75rem+env(safe-area-inset-bottom))]")}>
        <Button className="h-12 w-full justify-between px-4 aria-disabled:pointer-events-none aria-disabled:opacity-50 sm:h-11" asChild>
          {supporterPlan ? (
            <a href={supporterPlan.url} target="_blank" rel="noopener noreferrer" onClick={isDemo ? undefined : () => setAddingKey(true)}>
              <span className="font-medium">{t("support.checkout")}</span>
              <span className="tabular-nums opacity-90">{t("support.plans.yearly", { price: supporterPlan.price })}</span>
            </a>
          ) : (
            <a href={licenses ? themeLicenseCheckoutUrl() : undefined} aria-disabled={!licenses || undefined}>
              <span className="font-medium">{t("support.checkout")}</span>
              <span className="tabular-nums opacity-90">{t("support.fromPrice", { price: THEME_LICENSE_MIN_PRICE })}</span>
            </a>
          )}
        </Button>
        <p className="text-xs leading-5 text-muted-foreground">
          {supporterPlan ? t("support.terms.yearly", { price: supporterPlan.price }) : t("support.terms.license", { price: THEME_LICENSE_MIN_PRICE })}
        </p>
      </div>

      <div className="space-y-2.5 text-xs leading-5 text-muted-foreground max-sm:space-y-3 max-sm:text-[13px]">
        <p>{supporterPlan ? t("support.perks.yearly") : t("support.perks.license")}</p>
        {!isDemo && (
          <p>
            {t("support.existing")}{" "}
            <button type="button" className={LINK} onClick={() => setAddingKey(true)}>
              {t("support.addKey")}
            </button>
          </p>
        )}
        {supporterKey && supporterKey.status !== "invalid" && <p>{removeKey}</p>}
        {!isDemo && reminder.settled && !reminder.shortcutHidden && (
          <p>
            <Trans
              ns="common"
              i18nKey={reminder.reminderOff ? "support.reminder.notForYouHeart" : "support.reminder.notForYou"}
              components={{
                reminder: <button type="button" className={LINK} onClick={() => turnOffReminder(t)} />,
                heart: (
                  <button
                    type="button"
                    className={LINK}
                    onClick={() => {
                      onClose()
                      hideShortcut(t)
                    }}
                  />
                ),
              }}
            />
          </p>
        )}
      </div>
    </div>
  )
}

function SupporterKeyForm({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation("common")
  const activateLicense = useActivateLicense()
  const [key, setKey] = useState("")
  const inputId = useId()

  const submit = (event: FormEvent) => {
    event.preventDefault()
    activateLicense.mutate(key.trim(), { onSuccess: onDone })
  }

  return (
    <form className="space-y-4" onSubmit={submit}>
      <p className="text-sm leading-6 text-muted-foreground">{t("support.key.help")}</p>
      <div className="space-y-2">
        <Label htmlFor={inputId}>{t("support.key.label")}</Label>
        <Input
          id={inputId}
          value={key}
          onChange={(event) => setKey(event.target.value)}
          className="font-mono"
          autoComplete="off"
          spellCheck={false}
        />
        {activateLicense.isError && <LicenseError error={activateLicense.error} className="text-xs" />}
      </div>
      <div className="flex gap-2">
        <Button type="button" variant="outline" className="h-11 flex-1 sm:h-10" disabled={activateLicense.isPending} onClick={onDone}>
          {t("support.key.back")}
        </Button>
        <Button type="submit" className="h-11 flex-1 sm:h-10" disabled={!key.trim() || activateLicense.isPending}>
          {t("support.key.submit")}
        </Button>
      </div>
    </form>
  )
}
