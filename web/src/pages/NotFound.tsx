/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Logo } from "@/components/ui/Logo"
import { Link } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"

export function NotFound() {
  const { t } = useTranslation()

  return (
    <div className="flex items-center min-h-screen px-4 py-12 sm:px-6 md:px-8 lg:px-12 xl:px-16">
      <div className="w-full space-y-6 text-center">
        {/* Logo */}
        <div className="flex items-center justify-center mb-8">
          <Logo className="h-16 w-16 sm:h-20 sm:w-20" />
        </div>

        <div className="space-y-3">
          <h1 className="text-4xl font-bold tracking-tighter sm:text-5xl">{t("pages.notFound.title")}</h1>
          <p className="text-muted-foreground">{t("pages.notFound.description")}</p>
        </div>

        <div className="text-muted-foreground max-w-2xl mx-auto">
          <p>{t("pages.notFound.bugReport")}</p>
          <p>
            {t("pages.notFound.feelFree")}{" "}
            <a
              href="https://github.com/autobrr/qui/issues/new?template=bug_report.md"
              target="_blank"
              rel="noopener noreferrer"
              className="text-primary hover:text-primary/80 underline font-medium underline-offset-2 transition-colors"
            >
              {t("pages.notFound.github")}
            </a>
            .
          </p>
          <p className="pt-6">{t("pages.notFound.getBack")}</p>
        </div>

        <Button asChild className="h-10 px-8 text-sm font-medium">
          <Link to="/">
            {t("pages.notFound.return")}
          </Link>
        </Button>
      </div>
    </div>
  )
}