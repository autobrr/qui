/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTranslation } from "react-i18next"

import { gazelleLookupTextKey } from "@/lib/gazelle-lookup"
import type { CrossSeedGazelleLookup } from "@/types"

/** useGazelleLookupText returns a function that describes one RED/OPS entry: its plan or its outcome. */
export function useGazelleLookupText() {
  const { t } = useTranslation("torrents")
  return (lookup: CrossSeedGazelleLookup) => t(gazelleLookupTextKey(lookup), { site: lookup.shortName, detail: lookup.detail ?? "" })
}
