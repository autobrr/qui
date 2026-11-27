/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"

import { api } from "@/lib/api"

export const SENT_REPORTS_KEY = ["problem-reports", "sent"]
export const SEND_REPORT_KEY = ["problem-reports", "send"]

export function useSentReports() {
  return useQuery({
    queryKey: SENT_REPORTS_KEY,
    queryFn: () => api.getSentProblemReports(),
    staleTime: 0,
    refetchOnMount: "always",
    refetchOnReconnect: false,
    retry: false,
  })
}
