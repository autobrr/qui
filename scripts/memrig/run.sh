#!/usr/bin/env bash
# Memory rig: boots a stub qBittorrent and qui inside a loopback-only network
# namespace, drives a scenario, and records RSS and Go heap after each step.
#
#   run.sh <qui binary> <torrent count> <scenario file> <out dir> [webapi version]
#
# Start it through launch.sh, which adds the memory cap and the namespace.
set -u
BIN=$1; N=$2; SCEN=$3; OUT=$4; API=${5:-2.11.4}
RIG=$(dirname "$(readlink -f "$0")")
DATA=$HOME/.cache/qui-memrig/run-$$
mkdir -p "$OUT" "$DATA"
rm -f "$OUT/samples.txt" "$OUT/hook-codes.txt" "$OUT/list-codes.txt" "$OUT/sse-exits.txt" "$OUT/sse-errors.txt"
ip link set lo up

python3 "$RIG/stub.py" 18080 "$N" "$API" & STUB=$!
for _ in $(seq 100); do curl -fs 127.0.0.1:18080/api/v2/app/version >/dev/null && break; sleep 0.3; done

export QUI__PORT=17476 QUI__HOST=127.0.0.1 QUI__PPROF_ENABLED=true QUI__PPROF_ADDR=127.0.0.1:16060
export QUI__AUTH_DISABLED=true QUI__I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA=true QUI__AUTH_DISABLED_ALLOWED_CIDRS=127.0.0.0/8
export QUI__CHECK_FOR_UPDATES=false QUI__LOG_LEVEL=INFO
"$BIN" serve --config-dir "$DATA" --data-dir "$DATA" --log-path "$OUT/qui.log" & QUI=$!
trap 'kill $QUI $STUB 2>/dev/null; wait 2>/dev/null; rm -rf "$DATA"' EXIT

Q=http://127.0.0.1:17476
H=(-H 'X-Requested-With: XMLHttpRequest' -H 'Content-Type: application/json')
for _ in $(seq 100); do curl -fs $Q/health >/dev/null 2>&1 && break; sleep 0.3; done

# sample <label>: RSS, peak RSS, and the Go heap after a forced GC.
sample() {
  local rss hwm heap
  if ! kill -0 $QUI 2>/dev/null; then echo "$1 QUI-DIED" | tee -a "$OUT/samples.txt"; exit 1; fi
  if ! kill -0 $STUB 2>/dev/null; then echo "$1 STUB-DIED" | tee -a "$OUT/samples.txt"; exit 1; fi
  curl -fs "127.0.0.1:16060/debug/pprof/heap?gc=1" -o "$OUT/$1.heap"
  heap=$(curl -fs "127.0.0.1:16060/debug/pprof/heap?debug=1" | awk '/^# HeapInuse/{print int($4/1048576)}')
  rss=$(awk '/^VmRSS/{print int($2/1024)}' /proc/$QUI/status)
  hwm=$(awk '/^VmHWM/{print int($2/1024)}' /proc/$QUI/status)
  printf '%-28s rss=%5d MB  peak=%5d MB  heap_inuse=%5d MB  goroutines=%s\n' "$1" "$rss" "$hwm" "$heap" \
    "$(curl -fs '127.0.0.1:16060/debug/pprof/goroutine?debug=1' | head -1 | awk '{print $NF}')" | tee -a "$OUT/samples.txt"
}
# stream <seconds> <streams query>: one live-update stream in the background.
# It records the exit status of the stream and the number of sync errors that
# the stream got.
stream() {
  ( timeout "$1" curl -fsN "$Q/api/stream?streams=$2" | grep -a -c '^event: stream-error' >> "$OUT/sse-errors.txt"
    echo "${PIPESTATUS[0]}" >> "$OUT/sse-exits.txt" ) &
}
# checkstreams: stops the run when a stream ended before its timeout (an exit
# status other than 124) or got a sync error.
checkstreams() {
  sort "$OUT/sse-exits.txt" | uniq -c | tee -a "$OUT/samples.txt"
  if [ "$(sort -u "$OUT/sse-exits.txt")" != 124 ]; then echo "SSE-FAILED" | tee -a "$OUT/samples.txt"; exit 1; fi
  if [ "$(sort -u "$OUT/sse-errors.txt")" != 0 ]; then echo "SYNC-ERRORS" | tee -a "$OUT/samples.txt"; exit 1; fi
}
# reset the kernel's peak RSS counter so each step shows its own peak
resetpeak() { echo 5 > /proc/$QUI/clear_refs 2>/dev/null; }

echo "== N=$N api=$API bin=$(basename "$BIN")" | tee -a "$OUT/samples.txt"
sample boot
curl -s "${H[@]}" -X POST $Q/api/instances -d '{"name":"rig","host":"http://127.0.0.1:18080","username":"admin","password":"x","hasLocalFilesystemAccess":false}' > "$OUT/instance.json"
for _ in $(seq 240); do
  t=$(curl -s "$Q/api/instances/1/torrents?limit=1" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("total",0))' 2>/dev/null)
  [ "${t:-0}" = "$N" ] && break; sleep 0.5
done
if [ "${t:-0}" != "$N" ]; then echo "SYNC-FAILED total=${t:-?} want=$N" | tee -a "$OUT/samples.txt"; exit 1; fi
echo "synced total=$t" | tee -a "$OUT/samples.txt"
sample synced

. "$SCEN"

curl -s 127.0.0.1:18080/_counts > "$OUT/stub-counts.json"
echo done | tee -a "$OUT/samples.txt"
