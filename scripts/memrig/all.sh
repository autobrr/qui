#!/usr/bin/env bash
# all.sh <qui binary> <tag>: every scenario of the comparison, one after the other.
# Writes each run to $MEMRIG_OUT (default ~/.cache/qui-memrig/out)/<tag>-<scenario>.
# Exits with 1 when a scenario did not run to the end.
BIN=$(readlink -f "$1"); TAG=$2; RIG=$(dirname "$(readlink -f "$0")")
OUTDIR=${MEMRIG_OUT:-$HOME/.cache/qui-memrig/out}
mkdir -p "$OUTDIR"
FAILED=0
run() { "$RIG/launch.sh" "$BIN" "$1" "$RIG/$2" "$OUTDIR/$TAG-$3" > "$OUTDIR/$TAG-$3.log" 2>&1 || FAILED=1; }
run 20000 scen-webhook.sh 20k-hook
KS="8 16" run 40000 scen-webhook.sh 40k-hook
run 20000 scen-concurrency.sh 20k-conc
run 20000 scen-sse.sh 20k-sse
run 20000 scen-synccpu.sh 20k-synccpu
for d in 20k-hook 40k-hook 20k-conc 20k-sse 20k-synccpu; do echo "### $TAG $d"; cat "$OUTDIR/$TAG-$d/samples.txt"; done
exit "$FAILED"
