/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

import { createFileRoute } from '@tanstack/react-router'
import { Instances } from '@/pages/Instances'

export const Route = createFileRoute('/_authenticated/instances/')({
  component: Instances,
})