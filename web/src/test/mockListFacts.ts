/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ListFacts } from "@/contexts/TorrentSelectionContext"

// The list facts of a single instance with no categories, no tags, and the default sort.
export const LIST_FACTS: ListFacts = { isCrossInstance: false, allowSubcategories: false, sortField: "added_on", sortOrder: "desc" }
