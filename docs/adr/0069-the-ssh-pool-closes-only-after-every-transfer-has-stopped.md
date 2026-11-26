---
status: accepted
date: 2026-11-20
---

# The SSH pool closes only after every Transfer has stopped

At shutdown, qui asks each running Transfer to stop. A Transfer that stops keeps its partial data.

A Transfer that does not stop in time is waiting on a call that ignores the stop. If qui closed the SSH pool under it, the run could go on against a closed pool and finish a cleanup that leaves partial files behind. So qui closes the SSH pool only when every Transfer has stopped. Otherwise the pool stays open, and the exit of the process ends the run the way a crash does.

## Consequences

- The pool closes in the shutdown function, not in a `defer`, because the process ends with `os.Exit`.
- When the pool does close, the next read of another job gets `ErrConnectionLost` with `ErrPoolClosed` in its chain, and a read in flight gets `ErrConnectionLost`.
