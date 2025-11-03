/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createFileRoute } from '@tanstack/react-router'
import { IndexersPage } from '@/components/indexers/IndexersPage'

export const Route = createFileRoute('/_authenticated/indexers')({
  component: IndexersPage,
})
