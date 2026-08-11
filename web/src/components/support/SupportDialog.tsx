/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import {
  DODO_SUPPORTER_MONTHLY_PRICE,
  DODO_SUPPORTER_MONTHLY_URL,
  DODO_SUPPORTER_YEARLY_PRICE,
  DODO_SUPPORTER_YEARLY_URL
} from "@/lib/dodo-constants"
import { ArrowUpRight, Heart } from "lucide-react"
import { useTranslation } from "react-i18next"

type SupportDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function SupportDialog({ open, onOpenChange }: SupportDialogProps) {
  const { t } = useTranslation("common")

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md" overlayClassName="backdrop-blur-sm">
        <DialogHeader className="space-y-2">
          <DialogTitle className="flex items-center gap-2 text-base">
            <Heart className="h-4 w-4 fill-current text-red-500" />
            {t("support.title")}
          </DialogTitle>
          <DialogDescription className="text-sm leading-6">
            {t("support.description")}
          </DialogDescription>
          <p className="text-sm leading-6 text-muted-foreground">
            {t("support.perkPrefix")}{" "}
            <span className="whitespace-nowrap font-medium text-foreground">qui-patron</span>{" "}
            {t("support.perkSuffix")}
          </p>
        </DialogHeader>

        <div className="space-y-2">
          <Button className="w-full justify-between h-11 px-4" asChild>
            <a href={DODO_SUPPORTER_YEARLY_URL} target="_blank" rel="noopener noreferrer">
              <span className="font-medium">
                {t("support.yearly", { price: DODO_SUPPORTER_YEARLY_PRICE })}
              </span>
              <ArrowUpRight className="h-3.5 w-3.5" />
            </a>
          </Button>

          <Button variant="ghost" className="w-full justify-between h-9 px-4 text-muted-foreground" asChild>
            <a href={DODO_SUPPORTER_MONTHLY_URL} target="_blank" rel="noopener noreferrer">
              <span className="text-sm">
                {t("support.monthly", { price: DODO_SUPPORTER_MONTHLY_PRICE })}
              </span>
              <ArrowUpRight className="h-3.5 w-3.5" />
            </a>
          </Button>
        </div>

        <p className="border-t border-border/50 pt-4 text-xs leading-5 text-muted-foreground">
          {t("support.notLicense")}
        </p>
      </DialogContent>
    </Dialog>
  )
}
