/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE: three variants of the dashboard, switchable via `?variant=` on the
// existing /dashboard route. `?data=fixture|live` picks the data. Dev builds only.
// B2 is variant B after the Transfer history of #3991 lands.

import { PrototypeSwitcher } from "@/components/prototype/PrototypeSwitcher"
import { useNavigate } from "@tanstack/react-router"
import type { VariantProps } from "./shared"
import { VariantA, name as nameA } from "./VariantA"
import { VariantB, VariantB2, name as nameB } from "./VariantB"
import { VariantC, name as nameC } from "./VariantC"

const VARIANTS = [
  { key: "current", name: "Current" },
  { key: "a", name: `A (${nameA})`, Component: VariantA },
  { key: "b", name: `B (${nameB})`, Component: VariantB },
  { key: "b2", name: "B2 (after #3991)", Component: VariantB2 },
  { key: "c", name: `C (${nameC})`, Component: VariantC },
]

export const isPrototypeVariant = (variant: string | undefined) =>
  VARIANTS.some(v => v.key === variant && v.Component)

export function DashboardPrototype({ variant, ...props }: VariantProps & { variant: string | undefined }) {
  const navigate = useNavigate()
  const Component = VARIANTS.find(v => v.key === variant)?.Component
  const nextData = props.seedHistory ? "live" : "fixture"

  return (
    <>
      {Component && <Component {...props} />}
      <PrototypeSwitcher
        variants={VARIANTS}
        current={variant ?? "current"}
        toggle={{
          label: props.seedHistory ? "fixture data" : "live data",
          onClick: () => void navigate({ to: ".", search: (prev: Record<string, unknown>) => ({ ...prev, data: nextData }), replace: true }),
        }}
      />
    </>
  )
}
