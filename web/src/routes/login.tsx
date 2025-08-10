/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

import { createFileRoute } from '@tanstack/react-router'
import { Login } from '@/pages/Login'

export const Route = createFileRoute('/login')({
  component: Login,
})