/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Logo } from "@/components/ui/Logo"
import { Link } from "@tanstack/react-router"

export function NotFound() {
  return (
    <div className="flex items-center min-h-screen px-4 py-12 sm:px-6 md:px-8 lg:px-12 xl:px-16">
      <div className="w-full space-y-6 text-center">
        {/* Logo */}
        <div className="flex items-center justify-center mb-8">
          <Logo className="h-16 w-16 sm:h-20 sm:w-20" />
        </div>

        <div className="space-y-3">
          <h1 className="text-4xl font-bold tracking-tighter sm:text-5xl">Oops! Lost in the swarm?</h1>
          <p className="text-muted-foreground">Looks like you've ventured into the unknown.</p>
        </div>

        <div className="text-muted-foreground max-w-2xl mx-auto">
          <p>In case you think this is a bug rather than a missing link,</p>
          <p>
            feel free to report this to our{" "}
            <a
              href="https://github.com/autobrr/qui/issues/new?template=bug_report.md"
              target="_blank"
              rel="noopener noreferrer"
              className="text-primary hover:text-primary/80 underline font-medium underline-offset-2 transition-colors"
            >
              GitHub repository
            </a>
            .
          </p>
          <p className="pt-6">Otherwise, let us help you get back on track!</p>
        </div>

        <Button asChild className="h-10 px-8 text-sm font-medium">
          <Link to="/">
            Return to dashboard
          </Link>
        </Button>
      </div>
    </div>
  )
}