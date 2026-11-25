/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { setupServer } from "msw/node"

import { handlers } from "@/test/msw/handlers"

export const server = setupServer(...handlers)
