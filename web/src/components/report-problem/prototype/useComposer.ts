/*
 * PROTOTYPE, throwaway. Shared fake behavior for the variants: no network.
 */

import { useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import { draftReport, errorContext, sentReports, type SentReport } from "./mock"
import { setProto, useProto } from "./store"

export function useComposer(options: { onSent?: () => void } = {}) {
  const proto = useProto()
  const [text, setText] = useState("")
  const [logsIncluded, setLogsIncluded] = useState(true)
  const [image, setImageState] = useState<null | { name: string; url: string }>(null)
  const [logScope, setLogScope] = useState<"problems" | "all">(proto.source === "search" ? "all" : "problems")
  const [breakdownIncluded, setBreakdownIncluded] = useState(true)
  const [errorIncluded, setErrorIncluded] = useState(true)
  const [phase, setPhase] = useState<"idle" | "sending" | "blocked">("idle")
  const [signingIn, setSigningIn] = useState<null | "Discord" | "GitHub">(null)
  const timers = useRef<number[]>([])

  useEffect(() => () => timers.current.forEach(window.clearTimeout), [])
  useEffect(() => {
    setPhase("idle")
  }, [proto.identity, proto.lane, proto.source])
  // A missed cross-seed match shows in debug lines, not in warnings.
  useEffect(() => {
    setLogScope(proto.source === "search" ? "all" : "problems")
  }, [proto.source])

  function setImage(file: File | null) {
    if (image) URL.revokeObjectURL(image.url)
    setImageState(file ? { name: file.name || "Pasted image", url: URL.createObjectURL(file) } : null)
  }

  const anonymous = proto.identity === "anonymous"
  const hasError = proto.source === "error"
  const hasSearch = proto.source === "search"

  function later(ms: number, fn: () => void) {
    timers.current.push(window.setTimeout(fn, ms))
  }

  function send() {
    if (phase === "sending") return
    if (anonymous && proto.lane === "full") {
      setPhase("blocked")
      return
    }
    setPhase("sending")
    later(900, () => {
      setPhase("idle")
      setText("")
      toast.success("Report sent", { description: "Its status shows under Sent reports." })
      options.onSent?.()
    })
  }

  function signIn(provider: "Discord" | "GitHub") {
    setSigningIn(provider)
    later(1400, () => {
      setSigningIn(null)
      setProto({ identity: "signed-in" })
    })
  }

  return {
    text,
    setText,
    logsIncluded,
    setLogsIncluded,
    imageAllowed: !anonymous,
    image: anonymous ? null : image,
    setImage,
    logScope,
    setLogScope,
    hasSearch,
    breakdownIncluded: breakdownIncluded && hasSearch,
    setBreakdownIncluded,
    hasError,
    errorIncluded: errorIncluded && hasError,
    setErrorIncluded,
    error: errorContext,
    phase,
    send,
    signingIn,
    signIn,
    signOut: () => setProto({ identity: "anonymous" }),
    anonymous,
    canSend: text.trim().length > 0 && phase !== "sending" && !signingIn,
  }
}

export type Composer = ReturnType<typeof useComposer>

/** The sent list, with the unsent draft on top while the anonymous lane is full. */
export function useSentReports() {
  const proto = useProto()
  const [deleted, setDeleted] = useState<string[]>([])
  const rows: SentReport[] = (proto.lane === "full" ? [draftReport, ...sentReports] : sentReports).filter((r) => !deleted.includes(r.id))
  return { rows, remove: (id: string) => setDeleted((d) => [...d, id]) }
}
