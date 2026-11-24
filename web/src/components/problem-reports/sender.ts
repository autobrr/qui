/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { ProblemReportProvider, ProblemReportSender } from "@/types"

export const SENDER_KEY = ["problem-reports", "sender"]
const SIGN_IN_KEY = ["problem-reports", "sign-in"]
const SIGN_IN_POLL_MS = 2000
const POPUP_FEATURES = "popup,width=520,height=720"

export type SignInView =
  | { phase: "idle" }
  | { phase: "waiting"; provider: ProblemReportProvider }
  | { phase: "refused"; reason: string | undefined }
  | { phase: "expired" }
  | { phase: "popupBlocked" }
  | { phase: "failed"; error: Error }

export interface SenderControls {
  sender: ProblemReportSender | undefined
  signIn: SignInView
  startSignIn: (provider: ProblemReportProvider) => void
  signOut: () => void
}

class PopupBlockedError extends Error {}

export function useSender(): SenderControls {
  const { t } = useTranslation("common")
  const queryClient = useQueryClient()
  const [watched, setWatched] = useState<ProblemReportProvider | null>(null)
  const latestStart = useRef<AbortController | null>(null)

  const senderQuery = useQuery({
    queryKey: SENDER_KEY,
    queryFn: () => api.getProblemReportSender(),
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: true,
  })
  const startMutation = useMutation({
    mutationFn: async ({ provider, popup, start }: { provider: ProblemReportProvider; popup: Window | null; start: AbortController }) => {
      if (!popup) throw new PopupBlockedError()
      try {
        const { url } = await api.startProblemReportSignIn(provider)
        start.signal.throwIfAborted()
        popup.opener = null
        popup.location.href = url
      } catch (error) {
        popup.close()
        throw error
      }
    },
    onMutate: ({ popup, start }) => {
      if (!popup) return
      latestStart.current?.abort()
      latestStart.current = start
      setWatched(null)
      queryClient.removeQueries({ queryKey: SIGN_IN_KEY })
    },
    onSuccess: (_data, { provider }) => setWatched(provider),
  })
  const stateQuery = useQuery({
    queryKey: SIGN_IN_KEY,
    queryFn: async () => {
      const state = await api.getProblemReportSignInState()
      if (state.state === "done") await queryClient.invalidateQueries({ queryKey: SENDER_KEY })
      return state
    },
    enabled: watched !== null,
    refetchInterval: (query) => (query.state.data === undefined || query.state.data.state === "pending" ? SIGN_IN_POLL_MS : false),
    gcTime: 0,
  })
  const signOutMutation = useMutation({
    mutationFn: () => api.signOutOfProblemReports(),
    onMutate: () => {
      latestStart.current?.abort()
      setWatched(null)
      startMutation.reset()
    },
    onSuccess: async () => {
      await queryClient.cancelQueries({ queryKey: SENDER_KEY })
      queryClient.setQueryData<ProblemReportSender>(SENDER_KEY, { anonymous: true })
    },
    onError: (error) => toast.error(t("problemReports.sender.errors.signOutFailed", { message: error.message })),
  })

  return {
    sender: senderQuery.data,
    signIn: signInView(),
    startSignIn: (provider) => startMutation.mutate({ provider, popup: window.open("", "_blank", POPUP_FEATURES), start: new AbortController() }),
    signOut: () => signOutMutation.mutate(),
  }

  function signInView(): SignInView {
    if (startMutation.isPending) return { phase: "waiting", provider: startMutation.variables.provider }
    if (startMutation.error instanceof PopupBlockedError) return { phase: "popupBlocked" }
    if (startMutation.isError) return { phase: "failed", error: startMutation.error }
    if (watched === null) return { phase: "idle" }
    const state = stateQuery.data
    if (state === undefined || state.state === "pending") return { phase: "waiting", provider: watched }
    if (state.state === "refused") return { phase: "refused", reason: state.reason }
    if (state.state === "expired") return { phase: "expired" }
    return { phase: "idle" }
  }
}
