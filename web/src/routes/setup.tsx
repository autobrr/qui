/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

import { createFileRoute } from '@tanstack/react-router'
import { Setup } from '@/pages/Setup'

export const Route = createFileRoute('/setup')({
  component: Setup,
})